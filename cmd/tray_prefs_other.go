//go:build !darwin

package cmd

const hasPrefsWindow = false

func (a *trayApp) openPreferences() {}
