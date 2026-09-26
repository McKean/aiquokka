package cmd

import (
	"fmt"
	"time"
)

type prefsField int

const (
	prefNotify prefsField = iota
	prefNotifyReset
	prefThreshold
	prefPin
	prefInterval
)

type prefsView struct {
	notify      bool
	notifyReset bool
	thresholds  []string
	threshold   int
	pins        []string
	pinValues   []string
	pin         int
	intervals   []string
	interval    int
}

func intervalLabel(d time.Duration) string {
	if d%time.Minute == 0 {
		if n := int(d / time.Minute); n == 1 {
			return "1 minute"
		} else {
			return fmt.Sprintf("%d minutes", n)
		}
	}
	return d.String()
}

func (a *trayApp) prefsView() prefsView {
	v := prefsView{notify: a.notify, notifyReset: a.notifyReset}
	for i, t := range trayThresholdChoices {
		v.thresholds = append(v.thresholds, fmt.Sprintf("%d%%", t))
		if t == a.threshold {
			v.threshold = i
		}
	}
	v.pins = []string{"Highest of all providers"}
	v.pinValues = []string{""}
	for _, name := range pinChoices(a.lastResults, a.pinned) {
		v.pins = append(v.pins, name)
		v.pinValues = append(v.pinValues, name)
	}
	for i, value := range v.pinValues {
		if value == a.pinned {
			v.pin = i
		}
	}
	for i, d := range trayIntervalChoices {
		v.intervals = append(v.intervals, intervalLabel(d))
		if d == a.interval {
			v.interval = i
		}
	}
	return v
}

func (a *trayApp) applyPref(field prefsField, value int) {
	a.updateSettings(func() {
		v := a.shownPrefs
		switch field {
		case prefNotify:
			a.notify = value != 0
		case prefNotifyReset:
			a.notifyReset = value != 0
		case prefThreshold:
			if value >= 0 && value < len(trayThresholdChoices) {
				a.setThreshold(trayThresholdChoices[value])
			}
		case prefPin:
			if value >= 0 && value < len(v.pinValues) {
				a.pinned = v.pinValues[value]
			}
		case prefInterval:
			if value >= 0 && value < len(trayIntervalChoices) {
				a.interval = trayIntervalChoices[value]
			}
		}
	})
}
