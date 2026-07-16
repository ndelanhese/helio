// Package tariffs discovers tariff candidates from fixed, public sources.
package tariffs

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

const (
	CopelGroupBURL      = "https://www.copel.com/site/copel-distribuicao/tarifas-de-energia-eletrica/"
	copelParserVersion  = "copel-group-b-v1"
	defaultAvailability = 100
)

// Selection identifies the tariff row that applies to the installation.
type Selection struct {
	Class    string
	Subclass string
}

// ParsedCandidate preserves the parsed candidate and its fixed source.
type ParsedCandidate struct {
	Candidate domain.TariffProposal
}

var (
	tagPattern       = regexp.MustCompile(`(?is)<[^>]+>`)
	rowPattern       = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	cellPattern      = regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	effectivePattern = regexp.MustCompile(`(?i)vig[êe]ncia\s*[:\-]?\s*(\d{2}/\d{2}/\d{4})\s*(?:a|at[ée]|[-–])\s*(\d{2}/\d{2}/\d{4})`)
)

// ParseCopelGroupB parses a recorded Copel Group B page into a pending
// proposal. It deliberately accepts only the requested row and fails closed
// when its dates or component rates cannot be established.
func ParseCopelGroupB(page []byte, selection Selection, retrievedAt time.Time) (ParsedCandidate, error) {
	if len(bytes.TrimSpace(page)) == 0 {
		return ParsedCandidate{}, fmt.Errorf("parse Copel Group B: empty response")
	}
	if strings.TrimSpace(selection.Class) == "" || strings.TrimSpace(selection.Subclass) == "" {
		return ParsedCandidate{}, fmt.Errorf("parse Copel Group B: tariff selection is required")
	}
	dates := effectivePattern.FindSubmatch(page)
	if len(dates) != 3 {
		return ParsedCandidate{}, fmt.Errorf("parse Copel Group B: effective date range is required")
	}
	from, err := time.Parse("02/01/2006", string(dates[1]))
	if err != nil {
		return ParsedCandidate{}, fmt.Errorf("parse Copel Group B effective from: %w", err)
	}
	to, err := time.Parse("02/01/2006", string(dates[2]))
	if err != nil {
		return ParsedCandidate{}, fmt.Errorf("parse Copel Group B effective to: %w", err)
	}
	if to.Before(from) {
		return ParsedCandidate{}, fmt.Errorf("parse Copel Group B: effective date range is reversed")
	}
	te, tusd, err := selectedRates(page, selection)
	if err != nil {
		return ParsedCandidate{}, err
	}
	if te <= 0 || tusd <= 0 {
		return ParsedCandidate{}, fmt.Errorf("parse Copel Group B: selected rates must be positive")
	}
	candidate := domain.TariffProposal{
		Distributor:                 "COPEL",
		EffectiveFrom:               from.UTC(),
		EffectiveTo:                 to.UTC(),
		ConsumptionTEMicrosPerKWh:   te,
		ConsumptionTUSDMicrosPerKWh: tusd,
		AvailabilityKWh:             defaultAvailability,
		SourceURL:                   CopelGroupBURL,
		ParserVersion:               copelParserVersion,
		RetrievedAt:                 retrievedAt.UTC(),
	}
	if err := domain.ValidateTariffVersion(domain.TariffVersion{
		Distributor: candidate.Distributor, EffectiveFrom: candidate.EffectiveFrom, EffectiveTo: candidate.EffectiveTo,
		ConsumptionTEMicrosPerKWh: candidate.ConsumptionTEMicrosPerKWh, ConsumptionTUSDMicrosPerKWh: candidate.ConsumptionTUSDMicrosPerKWh,
		AvailabilityKWh: candidate.AvailabilityKWh,
	}); err != nil {
		return ParsedCandidate{}, fmt.Errorf("validate Copel candidate: %w", err)
	}
	return ParsedCandidate{Candidate: candidate}, nil
}

func selectedRates(page []byte, selection Selection) (int64, int64, error) {
	for _, row := range rowPattern.FindAllSubmatch(page, -1) {
		cells := cellPattern.FindAllSubmatch(row[1], -1)
		if len(cells) < 4 {
			continue
		}
		values := make([]string, len(cells))
		for i, cell := range cells {
			values[i] = normalizedText(cell[1])
		}
		if !strings.EqualFold(values[0], strings.TrimSpace(selection.Class)) || !sameSubclass(values[1], selection.Subclass) {
			continue
		}
		te, err := reaisToMicros(values[2])
		if err != nil {
			return 0, 0, fmt.Errorf("parse Copel Group B TE: %w", err)
		}
		tusd, err := reaisToMicros(values[3])
		if err != nil {
			return 0, 0, fmt.Errorf("parse Copel Group B TUSD: %w", err)
		}
		return te, tusd, nil
	}
	return 0, 0, fmt.Errorf("parse Copel Group B: selected tariff row not found")
}

func sameSubclass(source, selected string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	selected = strings.ToLower(strings.TrimSpace(selected))
	return source == selected || (source == "residencial" && selected == "residential")
}

func normalizedText(value []byte) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(tagPattern.ReplaceAllString(string(value), " "), "&nbsp;", " ")), " ")
}

func reaisToMicros(value string) (int64, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ".", ""))
	parts := strings.Split(value, ",")
	if len(parts) > 2 || len(parts) == 0 || parts[0] == "" {
		return 0, fmt.Errorf("invalid rate %q", value)
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return 0, fmt.Errorf("invalid rate %q", value)
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 6 {
		return 0, fmt.Errorf("rate has more than six decimal places")
	}
	if fraction == "" {
		fraction = "0"
	}
	for len(fraction) < 6 {
		fraction += "0"
	}
	fractionMicros, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid rate %q", value)
	}
	return whole*1_000_000 + fractionMicros, nil
}
