package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/ndelanhese/helio/internal/alerts"
	"github.com/ndelanhese/helio/internal/storage"
)

type alertEvidenceDTO struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type alertDTO struct {
	Kind       string             `json:"kind"`
	State      string             `json:"state"`
	Severity   alerts.Severity    `json:"severity"`
	Title      string             `json:"title"`
	Summary    string             `json:"summary"`
	OpenedAt   time.Time          `json:"openedAt"`
	ResolvedAt *time.Time         `json:"resolvedAt"`
	Evidence   []alertEvidenceDTO `json:"evidence"`
}

func (a *API) alerts(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state != "open" && state != "resolved" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_state", "state must be open or resolved")
		return
	}
	if a.dependencies.Alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "alerts are unavailable")
		return
	}
	records, err := a.dependencies.Alerts.List(r.Context(), state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "alerts could not be loaded")
		return
	}
	items := make([]alertDTO, 0, len(records))
	for _, record := range records {
		items = append(items, alertFromRecord(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": "v1", "state": state, "alerts": items})
}

func alertFromRecord(record storage.AlertRecord) alertDTO {
	title, summary := alertCopy(record.Rule, record.State)
	return alertDTO{Kind: record.Rule, State: record.State, Severity: record.Severity, Title: title, Summary: summary,
		OpenedAt: record.OpenedAt.UTC(), ResolvedAt: utcTime(record.ResolvedAt), Evidence: alertEvidence(record.Evidence)}
}

func utcTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := value.UTC()
	return &result
}

func alertCopy(rule, state string) (string, string) {
	if state == "resolved" {
		switch rule {
		case alerts.RuleLoggerOffline:
			return "Logger novamente disponível", "A comunicação com o logger foi restabelecida."
		case alerts.RuleTelemetryStale:
			return "Telemetria novamente atualizada", "As leituras voltaram a chegar dentro da janela esperada."
		case alerts.RuleInverterFault:
			return "Falha não está mais ativa", "Leituras novas confirmaram que a falha deixou de ser informada."
		case alerts.RuleZeroSunnyGeneration:
			return "Geração retomada", "Uma leitura nova registrou geração após a condição observada."
		case alerts.RulePersistentUnderproduction:
			return "Produção recuperada", "Dois dias qualificáveis voltaram à faixa de recuperação."
		case alerts.RuleGridVoltage:
			return "Tensão novamente na faixa", "A tensão retornou aos limites configurados."
		case alerts.RuleGridFrequency:
			return "Frequência novamente na faixa", "A frequência retornou aos limites configurados."
		}
	}
	switch rule {
	case alerts.RuleLoggerOffline:
		return "Logger sem comunicação", "Três tentativas consecutivas não receberam telemetria."
	case alerts.RuleTelemetryStale:
		return "Telemetria desatualizada", "A última leitura ultrapassou o limite de atualização."
	case alerts.RuleInverterFault:
		return "Falha informada pelo inversor", "Uma entrada ativa informou falha."
	case alerts.RuleZeroSunnyGeneration:
		return "Sem geração sob sol verificado", "A geração permaneceu zerada durante a janela de observação."
	case alerts.RulePersistentUnderproduction:
		return "Produção abaixo da referência", "A produção ficou abaixo da expectativa em dias qualificáveis consecutivos."
	case alerts.RuleGridVoltage:
		return "Tensão da rede fora da faixa", "A tensão permaneceu fora dos limites configurados."
	case alerts.RuleGridFrequency:
		return "Frequência da rede fora da faixa", "A frequência permaneceu fora dos limites configurados."
	default:
		return "Condição monitorada", "O Helio registrou uma condição interna."
	}
}

var safeEvidence = map[string]struct{ label, unit string }{
	"failed_polls":         {"Tentativas sem resposta", "polls"},
	"age_seconds":          {"Idade da leitura", "seconds"},
	"threshold_seconds":    {"Limite de atualização", "seconds"},
	"active_fault_sources": {"Entradas ativas com falha", "sources"},
	"power_w":              {"Potência observada", "W"},
	"elevation_degrees":    {"Elevação solar", "degrees"},
	"irradiance_wm2":       {"Irradiância", "W/m²"},
	"coverage_pct":         {"Cobertura da telemetria", "%"},
	"window_seconds":       {"Janela observada", "seconds"},
	"ratio":                {"Relação real/esperada", "ratio"},
	"qualifying_days":      {"Dias qualificáveis", "days"},
	"expected_wh":          {"Energia esperada", "Wh"},
	"actual_wh":            {"Energia medida", "Wh"},
	"voltage_v":            {"Tensão observada", "V"},
	"frequency_hz":         {"Frequência observada", "Hz"},
}

func alertEvidence(evidence alerts.Evidence) []alertEvidenceDTO {
	keys := make([]string, 0, len(evidence.Values))
	for key := range evidence.Values {
		if _, ok := safeEvidence[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := make([]alertEvidenceDTO, 0, len(keys))
	for _, key := range keys {
		copy := safeEvidence[key]
		result = append(result, alertEvidenceDTO{Label: copy.label, Value: evidence.Values[key], Unit: copy.unit})
	}
	return result
}
