package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/scoring"
	"github.com/nd28/journal-tui/internal/store"
)

type writingState struct {
	textarea      textarea.Model
	session       *scoring.Session
	lastWordCount int
	sessionID     int64
	startedAt     time.Time
	streakDays    int
	entryDate     string
	pasteWarning  string

	baselineWPM        float64
	hasBaseline        bool
	liveWPM            float64
	intensityRatio     float64
	peakIntensityRatio float64
}

const baselinePaceSessionWindow = 10

const (
	writingMaxWidth    = 100
	writingWidthMargin = 4
	writingMinWidth    = 20

	// writingChromeLines is the writing screen's fixed vertical overhead:
	// header, two blank separators, the help line, a line reserved for the
	// paste-block warning (even when not currently shown, so a paste
	// attempt never causes clipping), and the version footer appended by
	// Model.View().
	writingChromeLines  = 6
	writingHeightMargin = 2
	writingMinHeight    = 3

	compactWidth  = 40
	compactHeight = 6
)

// writingDimensions computes the textarea's width/height for the writing
// screen given the terminal size and whether compact mode is active.
// Compact mode ignores the terminal size entirely and returns
// bubbles/textarea's own built-in default (40x6) — today's v1 behavior.
func writingDimensions(termWidth, termHeight int, compact bool) (width, height int) {
	if compact {
		return compactWidth, compactHeight
	}

	width = termWidth
	if width > writingMaxWidth {
		width = writingMaxWidth
	}
	width -= writingWidthMargin
	if width < writingMinWidth {
		width = writingMinWidth
	}

	height = termHeight - writingChromeLines - writingHeightMargin
	if height < writingMinHeight {
		height = writingMinHeight
	}

	return width, height
}

type comboTickMsg time.Time

func comboTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return comboTickMsg(t)
	})
}

// syncWordCount reconciles a session's word count with the current text,
// awarding points for each newly completed word. Deletions (a lower word
// count) are not clawed back — points already earned stand — but the
// returned count is still updated so the next call computes the right delta.
func syncWordCount(sess *scoring.Session, prevWords int, text string, now time.Time) int {
	newWords := len(strings.Fields(text))
	for i := 0; i < newWords-prevWords; i++ {
		sess.CompleteWord(now)
	}
	return newWords
}

func renderComboBar(multiplier float64, width int) string {
	span := scoring.ComboCap - scoring.ComboFloor
	filled := int((multiplier - scoring.ComboFloor) / span * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return fmt.Sprintf("%s %.1fx", bar, multiplier)
}

func (m Model) startWritingSession() (tea.Model, tea.Cmd) {
	now := time.Now()
	today := now.Format("2006-01-02")
	newStreak := store.ComputeStreak(m.stats.LastEntryDate, today, m.stats.CurrentStreak)

	sessionID, err := m.store.StartSession(now)
	if err != nil {
		m.err = err
		return m, nil
	}

	baselineWPM, hasBaseline, err := m.store.RecentAvgPace(baselinePaceSessionWindow)
	if err != nil {
		m.err = err
		return m, nil
	}

	ta := textarea.New()
	ta.Placeholder = "Start writing..."
	ta.ShowLineNumbers = false
	// Prompt defaults to a 2-column "┃ " gutter that SetWidth reserves out
	// of the content width. Clearing it removes that reservation so the
	// textarea's content width matches writingDimensions exactly, and
	// reclaims those columns for actual writing space.
	ta.Prompt = ""
	w, h := writingDimensions(m.width, m.height, m.compactMode)
	ta.SetWidth(w)
	ta.SetHeight(h)
	focusCmd := ta.Focus()

	m.writing = writingState{
		textarea:    ta,
		session:     scoring.NewSession(now),
		sessionID:   sessionID,
		startedAt:   now,
		streakDays:  newStreak,
		entryDate:   today,
		baselineWPM: baselineWPM,
		hasBaseline: hasBaseline,
	}
	m.screen = screenWriting
	return m, tea.Batch(focusCmd, comboTick())
}

// finalizeCurrentEntry closes out the in-progress entry: it finalizes the
// scoring state and clears the textarea for the next entry, and reports the
// just-finished entry's text/word count so the caller can persist it
// immediately (rather than holding it in memory until the session ends).
// ok is false when there was nothing to save (an empty/untouched entry).
func (w *writingState) finalizeCurrentEntry() (body string, wordCount int, ok bool) {
	text := w.textarea.Value()
	words := w.lastWordCount

	w.session.NewEntry()
	w.textarea.Reset()
	w.lastWordCount = 0

	if strings.TrimSpace(text) == "" {
		return "", 0, false
	}
	return text, words, true
}

func (m Model) endWritingSession() (tea.Model, tea.Cmd) {
	if body, words, ok := m.writing.finalizeCurrentEntry(); ok {
		if err := m.store.SaveEntry(m.writing.sessionID, time.Now(), body, words); err != nil {
			m.err = err
		}
	}

	totalWords := m.writing.session.TotalWords()
	if totalWords == 0 {
		m.screen = screenHome
		m.homeCursor = 0
		return m, nil
	}

	raw := m.writing.session.RawScore()
	bonus := scoring.StreakBonus(m.writing.streakDays)
	final := scoring.FinalScore(raw, m.writing.streakDays)

	stats, isNewHigh, err := m.store.FinishSession(m.writing.sessionID, time.Now(), final, bonus, m.writing.streakDays, m.writing.entryDate)
	if err != nil {
		m.err = err
	} else {
		m.stats = stats

		avgPaceWPM := 0.0
		if duration := time.Since(m.writing.startedAt).Minutes(); duration > 0 {
			avgPaceWPM = float64(totalWords) / duration
		}
		if err := m.store.RecordSessionPace(m.writing.sessionID, avgPaceWPM, m.writing.peakIntensityRatio); err != nil {
			m.err = err
		}
	}

	m.summary = summaryState{
		rawScore:           raw,
		finalScore:         final,
		bonus:              bonus,
		totalWords:         totalWords,
		isNewHigh:          isNewHigh,
		peakIntensityRatio: m.writing.peakIntensityRatio,
	}
	m.screen = screenSummary
	return m, nil
}

func (m Model) updateWriting(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tickMsg, ok := msg.(comboTickMsg); ok {
		now := time.Time(tickMsg)
		m.writing.session.Combo.Tick(now)
		m.writing.liveWPM = m.writing.session.Pace.WPM(now)
		if m.writing.hasBaseline {
			m.writing.intensityRatio = m.writing.liveWPM / m.writing.baselineWPM
			if m.writing.intensityRatio > m.writing.peakIntensityRatio {
				m.writing.peakIntensityRatio = m.writing.intensityRatio
			}
		}
		return m, comboTick()
	}

	if _, ok := msg.(tea.WindowSizeMsg); ok {
		w, h := writingDimensions(m.width, m.height, m.compactMode)
		m.writing.textarea.SetWidth(w)
		m.writing.textarea.SetHeight(h)
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Paste {
			m.writing.pasteWarning = "paste disabled — write it yourself"
			return m, nil
		}
		m.writing.pasteWarning = ""

		switch keyMsg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc", "ctrl+d":
			return m.endWritingSession()
		case "ctrl+n":
			if body, words, ok := m.writing.finalizeCurrentEntry(); ok {
				if err := m.store.SaveEntry(m.writing.sessionID, time.Now(), body, words); err != nil {
					m.err = err
				}
			}
			return m, nil
		case "ctrl+t":
			m.compactMode = !m.compactMode
			w, h := writingDimensions(m.width, m.height, m.compactMode)
			m.writing.textarea.SetWidth(w)
			m.writing.textarea.SetHeight(h)
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.writing.textarea, cmd = m.writing.textarea.Update(msg)
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), time.Now())
	return m, cmd
}

func (m Model) viewWriting() string {
	combo := m.writing.session.Combo
	header := fmt.Sprintf(
		"Score: %s   Words: %s   %s",
		formatNumber(m.writing.session.RawScore()),
		formatNumber(m.writing.session.TotalWords()),
		renderComboBar(combo.Multiplier, 20),
	)
	if tier := scoring.IntensityTier(m.writing.intensityRatio); tier != "" {
		header += "   " + tier
	}
	header += "   " + formatPaceInfo(m.writing.liveWPM, m.writing.intensityRatio, m.writing.hasBaseline)
	help := statStyle.Render("ctrl+n: new entry   ctrl+t: toggle size   esc: end session")
	view := titleStyle.Render(header) + "\n\n" + m.writing.textarea.View() + "\n\n" + help
	if m.writing.pasteWarning != "" {
		view += "\n" + errorStyle.Render(m.writing.pasteWarning)
	}
	return view
}
