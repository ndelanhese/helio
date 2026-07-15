package sofar

type registerBlock struct {
	name  string
	start uint16
	count uint16
}

const (
	statusFaultStart = 0x0404
	statusFaultCount = 13
	statusOffset     = 0
	faultFirstOffset = 1
	faultWordCount   = 12

	gridStart           = 0x0484
	gridCount           = 10
	gridFrequencyOffset = 0
	acPowerOffset       = 1
	gridVoltageOffset   = 9

	pvStart          = 0x0584
	pvCount          = 6
	pv1VoltageOffset = 0
	pv1CurrentOffset = 1
	pv1PowerOffset   = 2
	pv2VoltageOffset = 3
	pv2CurrentOffset = 4
	pv2PowerOffset   = 5

	energyStart              = 0x0684
	energyCount              = 4
	energyTodayHighOffset    = 0
	energyTodayLowOffset     = 1
	energyLifetimeHighOffset = 2
	energyLifetimeLowOffset  = 3
)

func snapshotBlocks() [4]registerBlock {
	return [4]registerBlock{
		{name: "status/fault block", start: statusFaultStart, count: statusFaultCount},
		{name: "grid block", start: gridStart, count: gridCount},
		{name: "PV block", start: pvStart, count: pvCount},
		{name: "energy block", start: energyStart, count: energyCount},
	}
}
