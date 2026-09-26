package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/systray"
	"github.com/McKean/aiquokka/internal/usage"
	"github.com/gen2brain/beeep"
	"github.com/spf13/cobra"
	"golang.org/x/image/font"
)

type trayConfig struct {
	interval    time.Duration
	notify      bool
	threshold   int
	notifyReset bool
	pinned      string
}

var defaultTrayConfig = trayConfig{
	interval:    60 * time.Second,
	notify:      true,
	threshold:   80,
	notifyReset: true,
}

var (
	trayMinInterval      = time.Minute
	trayIntervalChoices  = []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute}
	trayThresholdChoices = []int{50, 60, 70, 75, 80, 85, 90, 95}
)

type limitLevel int

const (
	levelOK limitLevel = iota
	levelWarn
	levelCritical
	levelExhausted
)

func levelFor(pct float64, threshold int) limitLevel {
	switch {
	case pct >= 100:
		return levelExhausted
	case pct >= float64(threshold):
		return levelCritical
	case pct >= float64(threshold)*0.75:
		return levelWarn
	default:
		return levelOK
	}
}

func canonicalProvider(name string) (string, bool) {
	for _, p := range allProviders {
		if strings.EqualFold(p.name, name) {
			return p.name, true
		}
	}
	return "", false
}

func pinFor(pinned string, providers []provider) string {
	name, ok := canonicalProvider(pinned)
	if !ok {
		return ""
	}
	for _, p := range providers {
		if p.name == name {
			return name
		}
	}
	return ""
}

func windowPercent(w usage.Window) (float64, bool) {
	if w.UsedPercent != nil {
		return *w.UsedPercent, true
	}
	if w.Used != nil && w.Limit != nil && *w.Limit > 0 {
		return float64(*w.Used) / float64(*w.Limit) * 100, true
	}
	return 0, false
}

func newTrayCmd() *cobra.Command {
	cfg := defaultTrayConfig
	var specificProvider string

	cmd := &cobra.Command{
		Use:     "tray",
		Aliases: []string{"bar", "menu", "systray"},
		Short:   "Launch the macOS Menu Bar / Linux System Tray app",
		Long: `Launch aiquokka as a persistent menu bar (macOS) / system tray (Linux) app.

It sits quietly in your top bar, shows your most critical usage limit at a glance,
refreshes on a background timer, and alerts you via desktop notifications when limits
cross the warning threshold or reset.

The alert threshold, interval and notification toggles chosen in the menu are saved
and reused on the next launch; flags passed explicitly take precedence.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			providers := allProviders
			if specificProvider != "" {
				var filtered []provider
				for _, p := range allProviders {
					if strings.EqualFold(p.name, specificProvider) {
						filtered = append(filtered, p)
					}
				}
				if len(filtered) == 0 {
					return fmt.Errorf("unknown provider %q", specificProvider)
				}
				providers = filtered
			}
			if cfg.interval < trayMinInterval {
				return fmt.Errorf("--interval must be at least %s, the providers rate-limit faster polling", trayMinInterval)
			}
			if !validThreshold(cfg.threshold) {
				return fmt.Errorf("--threshold must be between 1 and 100, got %d", cfg.threshold)
			}
			if cfg.pinned != "" {
				name, ok := canonicalProvider(cfg.pinned)
				if !ok {
					return fmt.Errorf("--pin: unknown provider %q", cfg.pinned)
				}
				cfg.pinned = name
			}

			path := defaultTraySettingsPath()
			merged, err := loadTraySettings(path, cfg, cmd.Flags().Changed)
			if err != nil {
				fmt.Fprintf(os.Stderr, "aiquokka: ignoring tray settings: %v\n", err)
				merged = cfg
			}
			merged.pinned = pinFor(merged.pinned, providers)

			app := newTrayApp(providers, merged)
			app.settingsPath = path
			return app.run()
		},
	}

	cmd.Flags().DurationVarP(&cfg.interval, "interval", "i", defaultTrayConfig.interval, "refresh interval, at least 1m (e.g. 1m, 2m, 5m)")
	cmd.Flags().BoolVar(&cfg.notify, "notify", defaultTrayConfig.notify, "send desktop notifications when limits exceed threshold")
	cmd.Flags().IntVar(&cfg.threshold, "threshold", defaultTrayConfig.threshold, "percentage threshold (1-100) to trigger notifications")
	cmd.Flags().BoolVar(&cfg.notifyReset, "notify-reset", defaultTrayConfig.notifyReset, "send desktop notifications when a window resets")
	cmd.Flags().StringVarP(&specificProvider, "provider", "p", "", "watch only a specific provider (e.g. claude, agy, codex)")
	cmd.Flags().StringVar(&cfg.pinned, "pin", "", "provider shown in the menu bar (e.g. claude); default: the highest of all")

	return cmd
}

type trayApp struct {
	mu           sync.Mutex
	providers    []provider
	interval     time.Duration
	notify       bool
	threshold    int
	notifyReset  bool
	pinned       string
	settingsPath string

	alertedWindows map[string]limitLevel
	lastResetTimes map[string]time.Time
	send           func(title, body string, urgent bool)

	templateIcon []byte
	regularIcon  []byte
	glyphs       trayGlyphs
	face         font.Face

	refreshTrigger chan struct{}
	stopChan       chan struct{}
	menuCancel     context.CancelFunc
}

func newTrayApp(providers []provider, cfg trayConfig) *trayApp {
	tmpl, reg := getTrayIcons()
	return &trayApp{
		providers:      providers,
		interval:       cfg.interval,
		notify:         cfg.notify,
		threshold:      cfg.threshold,
		notifyReset:    cfg.notifyReset,
		pinned:         cfg.pinned,
		alertedWindows: make(map[string]limitLevel),
		lastResetTimes: make(map[string]time.Time),
		send:           sendDesktopNotification,
		templateIcon:   tmpl,
		regularIcon:    reg,
		glyphs:         newTrayGlyphs(),
		face:           tableFace(),
		refreshTrigger: make(chan struct{}, 1),
		stopChan:       make(chan struct{}),
	}
}

func tableFace() font.Face {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return loadRowFace()
}

func sendDesktopNotification(title, body string, urgent bool) {
	if urgent {
		_ = beeep.Alert(title, body, "")
		return
	}
	_ = beeep.Notify(title, body, "")
}

func (a *trayApp) config() trayConfig {
	return trayConfig{interval: a.interval, notify: a.notify, threshold: a.threshold, notifyReset: a.notifyReset, pinned: a.pinned}
}

func (a *trayApp) persist() {
	if err := saveTraySettings(a.settingsPath, a.config()); err != nil {
		fmt.Fprintf(os.Stderr, "aiquokka: could not save tray settings: %v\n", err)
	}
}

func (a *trayApp) run() error {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		systray.Quit()
	}()

	systray.Run(a.onReady, a.onExit)
	return nil
}

func (a *trayApp) onReady() {
	systray.SetTemplateIcon(a.templateIcon, a.regularIcon)
	systray.SetTitle("")
	systray.SetTooltip("aiquokka: AI Limits Watcher")

	go a.pollLoop()
}

func (a *trayApp) onExit() {
	a.mu.Lock()
	if a.menuCancel != nil {
		a.menuCancel()
	}
	a.mu.Unlock()
	close(a.stopChan)
}

func (a *trayApp) triggerRefresh() {
	select {
	case a.refreshTrigger <- struct{}{}:
	default:
	}
}

func (a *trayApp) pollLoop() {
	a.fetchAndUpdate()

	for {
		a.mu.Lock()
		interval := a.interval
		a.mu.Unlock()

		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
			a.fetchAndUpdate()
		case <-a.refreshTrigger:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			a.fetchAndUpdate()
		case <-a.stopChan:
			timer.Stop()
			return
		}
	}
}

type traySummary struct {
	active          int
	highestPct      float64
	highestProvider string
	highestWindow   string
	highestResetsAt time.Time
	overThreshold   int
}

func summarize(results []fetchResult, threshold int) traySummary {
	s := traySummary{highestPct: -1}
	for _, res := range results {
		if res.err != nil {
			if !usage.IsNotConfigured(res.err) {
				s.active++
			}
			continue
		}
		s.active++
		for _, w := range res.report.Windows {
			pct, ok := windowPercent(w)
			if !ok {
				continue
			}
			if pct >= float64(threshold) {
				s.overThreshold++
			}
			if pct > s.highestPct {
				s.highestPct = pct
				s.highestProvider = res.report.Provider
				s.highestWindow = w.Label
				s.highestResetsAt = w.ResetsAt
			}
		}
	}
	return s
}

func (a *trayApp) fetchAndUpdate() {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	results := fetchAll(ctx, a.providers)
	now := time.Now()

	a.mu.Lock()
	defer a.mu.Unlock()

	for _, res := range results {
		if res.err != nil {
			continue
		}
		for _, w := range res.report.Windows {
			a.checkNotifications(res.report.Provider, w, now)
		}
	}

	s := summarize(results, a.threshold)
	if a.pinned != "" {
		a.updatePinnedStatusItem(pinnedStatus(results, a.pinned), now)
	} else {
		a.updateStatusItem(s, now)
	}
	a.rebuildMenu(results, now, s)
}

func (a *trayApp) updateStatusItem(s traySummary, now time.Time) {
	switch {
	case s.active == 0:
		systray.SetTemplateIcon(a.templateIcon, a.regularIcon)
		systray.SetTitle("")
		systray.SetTooltip("aiquokka: No configured AI providers detected")
	case s.highestPct < 0:
		systray.SetTemplateIcon(a.templateIcon, a.regularIcon)
		systray.SetTitle("")
		systray.SetTooltip("aiquokka: All AI providers monitored")
	default:
		g := gaugeGlyph(s.highestPct, -1, levelFor(s.highestPct, a.threshold) >= levelCritical)
		systray.SetTemplateIcon(g.template, g.regular)
		systray.SetTitle(" " + formatPct(s.highestPct))

		resetInfo := ""
		if !s.highestResetsAt.IsZero() {
			resetInfo = fmt.Sprintf(", resets %s", usage.HumanizeReset(s.highestResetsAt, now))
		}
		systray.SetTooltip(fmt.Sprintf("aiquokka: %s %s at %s%s · alert at %d%%",
			s.highestProvider, s.highestWindow, formatPct(s.highestPct), resetInfo, a.threshold))
	}
}

type pinnedView struct {
	provider   string
	configured bool
	err        error
	pct        float64
	window     string
	resetsAt   time.Time
}

func pinnedStatus(results []fetchResult, provider string) pinnedView {
	v := pinnedView{provider: provider, pct: -1}
	for _, res := range results {
		if res.name != provider {
			continue
		}
		if res.err != nil {
			if !usage.IsNotConfigured(res.err) {
				v.configured, v.err = true, res.err
			}
			return v
		}
		v.configured = true
		for _, w := range res.report.Windows {
			if pct, ok := windowPercent(w); ok && pct > v.pct {
				v.pct, v.window, v.resetsAt = pct, w.Label, w.ResetsAt
			}
		}
		return v
	}
	return v
}

func (a *trayApp) updatePinnedStatusItem(v pinnedView, now time.Time) {
	logo := logoGlyph(v.provider)
	switch {
	case !v.configured:
		systray.SetTemplateIcon(logo.template, logo.regular)
		systray.SetTitle(" —")
		systray.SetTooltip(fmt.Sprintf("aiquokka: %s is not configured", v.provider))
	case v.err != nil:
		systray.SetTemplateIcon(logo.template, logo.regular)
		systray.SetTitle(" —")
		systray.SetTooltip(fmt.Sprintf("aiquokka: %s: %v", v.provider, v.err))
	case v.pct < 0:
		systray.SetTemplateIcon(logo.template, logo.regular)
		systray.SetTitle("")
		systray.SetTooltip(fmt.Sprintf("aiquokka: %s has no usage percentage", v.provider))
	default:
		if levelFor(v.pct, a.threshold) >= levelCritical {
			logo = alertLogoGlyph(v.provider)
		}
		systray.SetTemplateIcon(logo.template, logo.regular)
		systray.SetTitle(" " + formatPct(v.pct))
		resetInfo := ""
		if !v.resetsAt.IsZero() {
			resetInfo = fmt.Sprintf(", resets %s", usage.HumanizeReset(v.resetsAt, now))
		}
		systray.SetTooltip(fmt.Sprintf("aiquokka: %s %s at %s%s · alert at %d%%",
			v.provider, v.window, formatPct(v.pct), resetInfo, a.threshold))
	}
}

func pinChoices(results []fetchResult, pinned string) []string {
	var names []string
	seen := map[string]bool{}
	for _, res := range configuredResults(results) {
		if !seen[res.name] {
			seen[res.name] = true
			names = append(names, res.name)
		}
	}
	if pinned != "" && !seen[pinned] {
		names = append(names, pinned)
	}
	return names
}

func summaryLine(s traySummary, threshold int) (string, bool) {
	switch {
	case s.overThreshold == 1:
		return fmt.Sprintf("1 limit at or above %d%%", threshold), true
	case s.overThreshold > 1:
		return fmt.Sprintf("%d limits at or above %d%%", s.overThreshold, threshold), true
	default:
		return fmt.Sprintf("All limits below %d%%", threshold), false
	}
}

func addRow(text, tooltip string, g glyph) *systray.MenuItem {
	item := systray.AddMenuItem(text, tooltip)
	item.SetTemplateIcon(g.template, g.regular)
	return item
}

func addImageRow(img []byte, tooltip string) *systray.MenuItem {
	item := systray.AddMenuItem("", tooltip)
	item.SetTemplateIcon(img, img)
	return item
}

func configuredResults(results []fetchResult) []fetchResult {
	var out []fetchResult
	for _, res := range results {
		if res.err == nil || !usage.IsNotConfigured(res.err) {
			out = append(out, res)
		}
	}
	return out
}

func (a *trayApp) addProviderSections(results []fetchResult, now time.Time) {
	sources := configuredResults(results)
	if a.face == nil {
		a.addTextSections(sources, now)
		return
	}

	var allRows []usageRow
	var allFacts []usage.Fact
	for _, src := range sources {
		if src.err != nil {
			continue
		}
		for _, w := range src.report.Windows {
			allRows = append(allRows, newUsageRow(w, now, a.threshold))
		}
		allFacts = append(allFacts, src.report.Extra...)
	}
	layout := newTableLayout(a.face, allRows, allFacts)

	for _, src := range sources {
		if src.err != nil {
			addImageRow(renderHeaderRow(a.face, logoGlyph(src.name).template, src.name, "", 0), "")
			addRow(src.err.Error(), "", a.glyphs.warning).Disable()
			systray.AddSeparator()
			continue
		}
		rep := src.report
		addImageRow(renderHeaderRow(a.face, logoGlyph(rep.Provider).template, rep.Provider, rep.Plan, 0), rep.Plan)
		if len(rep.Windows) == 0 {
			addRow("No usage windows reported", "", a.glyphs.spacer).Disable()
		}
		for _, w := range rep.Windows {
			row := newUsageRow(w, now, a.threshold)
			addImageRow(renderUsageRow(layout, row, a.glyphs.warning.template), row.tooltip)
		}
		for _, f := range rep.Extra {
			addImageRow(renderFactRow(layout, f), "")
		}
		systray.AddSeparator()
	}
}

func (a *trayApp) addTextSections(sources []fetchResult, now time.Time) {
	for _, src := range sources {
		if src.err != nil {
			addRow(src.name, "", logoGlyph(src.name)).Disable()
			addRow(src.err.Error(), "", a.glyphs.warning).Disable()
			systray.AddSeparator()
			continue
		}
		rep := src.report
		header := rep.Provider
		if rep.Plan != "" {
			header = fmt.Sprintf("%s  ·  %s", rep.Provider, rep.Plan)
		}
		addRow(header, "", logoGlyph(rep.Provider)).Disable()
		if len(rep.Windows) == 0 {
			addRow("No usage windows reported", "", a.glyphs.spacer).Disable()
		}
		for _, w := range rep.Windows {
			addRow(windowTitle(w, now), windowTooltip(w, now), a.windowGlyph(w, now))
		}
		for _, extra := range rep.Extra {
			addRow(fmt.Sprintf("%s: %s", extra.Label, extra.Value), "", a.glyphs.spacer)
		}
		systray.AddSeparator()
	}
}

func (a *trayApp) rebuildMenu(results []fetchResult, now time.Time, s traySummary) {
	if a.menuCancel != nil {
		a.menuCancel()
	}
	menuCtx, cancel := context.WithCancel(context.Background())
	a.menuCancel = cancel

	systray.ResetMenu()

	if s.active == 0 {
		addRow("No configured providers found", "Log in via the official CLIs", a.glyphs.warning).Disable()
		systray.AddSeparator()
	} else {
		text, alert := summaryLine(s, a.threshold)
		summaryGlyph := a.glyphs.ok
		if alert {
			summaryGlyph = a.glyphs.warning
		}
		addRow(text, "Change it in Preferences → Alert at", summaryGlyph)
		systray.AddSeparator()

		a.addProviderSections(results, now)
	}

	mRefresh := addRow(fmt.Sprintf("Refresh now  ·  checked %s", now.Format("15:04")), "Check all limits immediately", a.glyphs.refresh)

	mSettings := addRow("Preferences", "Adjust alerts and interval", a.glyphs.prefs)
	mAlerts := mSettings.AddSubMenuItemCheckbox("Desktop alerts", "Notify when a limit crosses the alert threshold", a.notify)
	mResetAlerts := mSettings.AddSubMenuItemCheckbox("Notify on reset", "Notify when a window's allowance renews", a.notifyReset)

	mThresholdSub := mSettings.AddSubMenuItem(fmt.Sprintf("Alert at %d%%", a.threshold), "Usage percentage that triggers an alert")
	thresholdItems := make(map[int]*systray.MenuItem)
	for _, t := range trayThresholdChoices {
		thresholdItems[t] = mThresholdSub.AddSubMenuItemCheckbox(fmt.Sprintf("%d%%", t), "", t == a.threshold)
	}

	pinLabel := "highest of all"
	if a.pinned != "" {
		pinLabel = a.pinned
	}
	mPinSub := mSettings.AddSubMenuItem(fmt.Sprintf("Menu bar shows %s", pinLabel), "Choose what the menu bar shows")
	pinItems := map[string]*systray.MenuItem{
		"": mPinSub.AddSubMenuItemCheckbox("Highest of all", "The highest usage of all providers", a.pinned == ""),
	}
	for _, name := range pinChoices(results, a.pinned) {
		pinItems[name] = mPinSub.AddSubMenuItemCheckbox(name, "Show only "+name+" in the menu bar", a.pinned == name)
	}

	mIntervalSub := mSettings.AddSubMenuItem(fmt.Sprintf("Refresh every %s", shortDuration(a.interval)), "Polling interval")
	intervalItems := make(map[time.Duration]*systray.MenuItem)
	for _, dur := range trayIntervalChoices {
		intervalItems[dur] = mIntervalSub.AddSubMenuItemCheckbox(shortDuration(dur), "", dur == a.interval)
	}

	systray.AddSeparator()
	mQuit := addRow("Quit aiquokka", "Close the menu bar app", a.glyphs.quit)

	a.listenMenuEvents(menuCtx, trayMenu{
		refresh:     mRefresh,
		alerts:      mAlerts,
		resetAlerts: mResetAlerts,
		quit:        mQuit,
		thresholds:  thresholdItems,
		intervals:   intervalItems,
		pins:        pinItems,
	})
}

func shortDuration(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	default:
		return d.String()
	}
}

func formatPct(pct float64) string {
	if pct > 0 && pct < 1 {
		return fmt.Sprintf("%.1f%%", pct)
	}
	return fmt.Sprintf("%.0f%%", pct)
}

func (a *trayApp) windowGlyph(w usage.Window, now time.Time) glyph {
	if pct, ok := windowPercent(w); ok {
		return gaugeGlyph(pct, w.Pace(now), levelFor(pct, a.threshold) >= levelCritical)
	}
	if w.Remaining != nil {
		if *w.Remaining <= 0 {
			return gaugeGlyph(0, -1, true)
		}
		return gaugeGlyph(100, -1, false)
	}
	return a.glyphs.unknown
}

func windowTitle(w usage.Window, now time.Time) string {
	var b strings.Builder
	b.WriteString(w.Label)
	b.WriteString("   ")
	pct, hasPct := windowPercent(w)
	switch {
	case hasPct:
		b.WriteString(formatPct(pct))
		if w.UsedPercent == nil {
			fmt.Fprintf(&b, "  (%d/%d)", *w.Used, *w.Limit)
		}
	case w.Remaining != nil:
		b.WriteString(usage.FormatMoney(*w.Remaining, w.Currency))
	default:
		b.WriteString("—")
	}
	if !w.ResetsAt.IsZero() {
		fmt.Fprintf(&b, "   ·   resets %s", usage.HumanizeReset(w.ResetsAt, now))
	}
	return b.String()
}

func windowTooltip(w usage.Window, now time.Time) string {
	pct, ok := windowPercent(w)
	pace := w.Pace(now)
	if !ok || pace < 0 {
		return ""
	}
	elapsed := pace * 100
	diff := pct - elapsed
	switch {
	case diff >= 2:
		return fmt.Sprintf("%.0f%% of the window elapsed · %.0f pts over pace", elapsed, diff)
	case diff <= -2:
		return fmt.Sprintf("%.0f%% of the window elapsed · %.0f pts under pace", elapsed, -diff)
	default:
		return fmt.Sprintf("%.0f%% of the window elapsed · on pace", elapsed)
	}
}

type trayMenu struct {
	refresh     *systray.MenuItem
	alerts      *systray.MenuItem
	resetAlerts *systray.MenuItem
	quit        *systray.MenuItem
	thresholds  map[int]*systray.MenuItem
	intervals   map[time.Duration]*systray.MenuItem
	pins        map[string]*systray.MenuItem
}

func onClick(ctx context.Context, item *systray.MenuItem, fn func()) {
	go func() {
		select {
		case <-item.ClickedCh:
			fn()
		case <-ctx.Done():
		}
	}()
}

func (a *trayApp) updateSettings(fn func()) {
	a.mu.Lock()
	fn()
	a.persist()
	a.mu.Unlock()
	a.triggerRefresh()
}

func (a *trayApp) setThreshold(t int) {
	a.threshold = t
	a.alertedWindows = make(map[string]limitLevel)
}

func (a *trayApp) listenMenuEvents(ctx context.Context, m trayMenu) {
	onClick(ctx, m.refresh, a.triggerRefresh)
	onClick(ctx, m.alerts, func() { a.updateSettings(func() { a.notify = !a.notify }) })
	onClick(ctx, m.resetAlerts, func() { a.updateSettings(func() { a.notifyReset = !a.notifyReset }) })
	for t, item := range m.thresholds {
		onClick(ctx, item, func() { a.updateSettings(func() { a.setThreshold(t) }) })
	}
	for d, item := range m.intervals {
		onClick(ctx, item, func() { a.updateSettings(func() { a.interval = d }) })
	}
	for name, item := range m.pins {
		onClick(ctx, item, func() { a.updateSettings(func() { a.pinned = name }) })
	}
	onClick(ctx, m.quit, systray.Quit)
}

func (a *trayApp) checkNotifications(providerName string, win usage.Window, now time.Time) {
	key := fmt.Sprintf("%s:%s", providerName, win.Label)

	if pct, ok := windowPercent(win); ok {
		lvl := levelFor(pct, a.threshold)
		switch {
		case lvl >= levelCritical && lvl > a.alertedWindows[key]:
			a.alertedWindows[key] = lvl
			if a.notify {
				a.send(thresholdAlert(providerName, win, pct, lvl, now))
			}
		case pct < float64(a.threshold)-10:
			delete(a.alertedWindows, key)
		}
	}

	if !win.ResetsAt.IsZero() {
		lastReset, known := a.lastResetTimes[key]
		if known && win.ResetsAt.After(lastReset) && a.notify && a.notifyReset {
			a.send(
				fmt.Sprintf("aiquokka · %s", providerName),
				fmt.Sprintf("%s limit has been reset.", win.Label),
				false,
			)
		}
		a.lastResetTimes[key] = win.ResetsAt
	}
}

func thresholdAlert(providerName string, win usage.Window, pct float64, lvl limitLevel, now time.Time) (string, string, bool) {
	resetMsg := ""
	if !win.ResetsAt.IsZero() {
		resetMsg = fmt.Sprintf(" Resets %s.", usage.HumanizeReset(win.ResetsAt, now))
	}
	title := fmt.Sprintf("aiquokka · %s", providerName)
	if lvl == levelExhausted {
		return title, fmt.Sprintf("%s limit reached (%s).%s", win.Label, formatPct(pct), resetMsg), true
	}
	return title, fmt.Sprintf("%s is at %s of its quota.%s", win.Label, formatPct(pct), resetMsg), false
}
