package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"journal/internal/scoring"
	"journal/internal/store"
)

type writingState struct {
	textarea      textarea.Model
	session       *scoring.Session
	lastWordCount int
	sessionID     int64
	startedAt     time.Time
	streakDays    int
	entryDate     string
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

	ta := textarea.New()
	ta.Placeholder = "Start writing..."
	ta.ShowLineNumbers = false
	focusCmd := ta.Focus()

	m.writing = writingState{
		textarea:   ta,
		session:    scoring.NewSession(now),
		sessionID:  sessionID,
		startedAt:  now,
		streakDays: newStreak,
		entryDate:  today,
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

	raw := m.writing.session.RawScore()
	bonus := scoring.StreakBonus(m.writing.streakDays)
	final := scoring.FinalScore(raw, m.writing.streakDays)
	totalWords := m.writing.session.TotalWords()

	stats, isNewHigh, err := m.store.FinishSession(m.writing.sessionID, time.Now(), final, bonus, m.writing.streakDays, m.writing.entryDate)
	if err != nil {
		m.err = err
	} else {
		m.stats = stats
	}

	m.summary = summaryState{
		rawScore:   raw,
		finalScore: final,
		bonus:      bonus,
		totalWords: totalWords,
		isNewHigh:  isNewHigh,
	}
	m.screen = screenSummary
	return m, nil
}

func (m Model) updateWriting(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tickMsg, ok := msg.(comboTickMsg); ok {
		m.writing.session.Combo.Tick(time.Time(tickMsg))
		return m, comboTick()
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
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
		"Score: %d   Words: %d   %s",
		m.writing.session.RawScore(),
		m.writing.session.TotalWords(),
		renderComboBar(combo.Multiplier, 20),
	)
	help := statStyle.Render("ctrl+n: new entry   esc: end session")
	return titleStyle.Render(header) + "\n\n" + m.writing.textarea.View() + "\n\n" + help
}
