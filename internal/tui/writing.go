package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
	paceSampler        scoring.PaceSampler
}

const baselinePaceSessionWindow = 10

const pasteWarningText = "paste disabled — write it yourself"

// maxWordsPerUpdate bounds how many words one update can score. Typing adds
// at most a word per keystroke, so a larger jump means text arrived in bulk
// — a paste path that slipped past the guard. Unbounded, such a jump awards
// thousands of points and stamps thousands of words at a single instant,
// which spikes the pace reading to an impossible value and poisons the
// personal baseline derived from it.
const maxWordsPerUpdate = 20

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

// completedWords counts the words in text that are finished — that is,
// followed by whitespace. A word still being typed isn't counted, so a word
// isn't scored the instant its first letter lands and pace readings measure
// words actually written rather than words started.
func completedWords(text string) int {
	n := len(strings.Fields(text))
	if n == 0 {
		return 0
	}
	if last, _ := utf8.DecodeLastRuneInString(text); !unicode.IsSpace(last) {
		n--
	}
	return n
}

// syncWordCount reconciles a session's scoring state with the current text.
// highWater is the most completed words this entry has ever held; only words
// past it score, so deleting a paragraph and retyping it can't collect the
// same points twice. Editing mid-document is unaffected: the count only has
// to exceed its own previous maximum. Returns the new high-water mark.
func syncWordCount(sess *scoring.Session, highWater int, text string, now time.Time) int {
	words := completedWords(text)
	if words <= highWater {
		return highWater
	}

	gained := words - highWater
	if gained > maxWordsPerUpdate {
		gained = maxWordsPerUpdate
	}
	for i := 0; i < gained; i++ {
		sess.CompleteWord(now)
	}

	// The mark moves to the full count even when the award was clamped: the
	// unscored remainder is bulk-inserted text, not writing, and must not
	// trickle into the score on subsequent keystrokes.
	return words
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
	// The textarea binds ctrl+v to its own clipboard paste, which arrives as
	// an internal message rather than a key event — so it slips straight past
	// the tea.KeyMsg.Paste guard in updateWriting, which only ever sees
	// terminal-driven bracketed pastes. Disable the binding at the source.
	ta.KeyMap.Paste.SetEnabled(false)
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
	// Count what the saved text actually holds, not the scoring high-water
	// mark: the mark deliberately ignores deletions, so after heavy editing
	// it overstates what's on the page.
	words := len(strings.Fields(text))

	w.session.NewEntry()
	w.textarea.Reset()
	w.lastWordCount = 0

	if strings.TrimSpace(text) == "" {
		return "", 0, false
	}
	return text, words, true
}

func (m Model) endWritingSession() (tea.Model, tea.Cmd) {
	savedEntry := false
	if body, words, ok := m.writing.finalizeCurrentEntry(); ok {
		savedEntry = true
		if err := m.store.SaveEntry(m.writing.sessionID, time.Now(), body, words); err != nil {
			m.err = err
		}
	}

	totalWords := m.writing.session.TotalWords()
	if totalWords == 0 && !savedEntry {
		// Nothing was written, so the row StartSession inserted is noise.
		// Drop it rather than leaving an unfinished session behind forever
		// — opening the writing screen and changing your mind shouldn't
		// leave a trace. The savedEntry guard keeps a session that holds
		// text but no completed word (a single unterminated word) from
		// taking its own writing down with it.
		if err := m.store.DiscardSession(m.writing.sessionID); err != nil {
			m.err = err
		}
		m.screen = screenHome
		m.homeCursor = 0
		return m, nil
	}

	raw := m.writing.session.RawScore()
	bonus := scoring.StreakBonus(m.writing.streakDays)
	final := scoring.FinalScore(raw, m.writing.streakDays)

	// The median of the pace readings taken while actually typing, not total
	// words over wall-clock duration. The live readings this session's
	// baseline will be compared against are burst measurements, so the
	// baseline has to be one too; a wall-clock average counts every pause as
	// slow writing and drags the baseline toward zero, which inflates every
	// future ratio. With no readings at all the session was too short to
	// measure, and both the stored columns and the summary report nothing
	// rather than a pace of zero.
	activePaceWPM, hasPace := m.writing.paceSampler.Median()

	stats, isNewHigh, err := m.store.FinishSession(m.writing.sessionID, time.Now(), final, bonus, m.writing.streakDays, m.writing.entryDate)
	if err != nil {
		m.err = err
	} else {
		m.stats = stats

		if hasPace {
			if err := m.store.RecordSessionPace(m.writing.sessionID, activePaceWPM, m.writing.peakIntensityRatio); err != nil {
				m.err = err
			}
		}
	}

	m.summary = summaryState{
		rawScore:           raw,
		finalScore:         final,
		bonus:              bonus,
		totalWords:         totalWords,
		isNewHigh:          isNewHigh,
		peakIntensityRatio: m.writing.peakIntensityRatio,
		sessionPaceWPM:     activePaceWPM,
		hasSessionPace:     hasPace,
		hasBaseline:        m.writing.hasBaseline,
	}
	m.screen = screenSummary
	return m, nil
}

func (m Model) updateWriting(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tickMsg, ok := msg.(comboTickMsg); ok {
		now := time.Time(tickMsg)
		m.writing.session.Combo.Tick(now)
		m.writing.liveWPM = m.writing.session.Pace.WPM(now)
		if m.writing.session.Pace.Active(now) {
			m.writing.paceSampler.Sample(m.writing.liveWPM)
		}
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
			m.writing.pasteWarning = pasteWarningText
			return m, nil
		}
		m.writing.pasteWarning = ""

		switch keyMsg.String() {
		case "ctrl+v":
			// The textarea's own paste binding is disabled, so this key
			// would otherwise do nothing silently. Say why.
			m.writing.pasteWarning = pasteWarningText
			return m, nil
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
	// LiveTier always returns a word, so this is unconditional: the
	// qualitative reading is meant to be a constant presence beside the
	// numbers rather than something that appears only during a fast burst.
	header += "   " + scoring.LiveTier(m.writing.liveWPM, m.writing.intensityRatio, m.writing.hasBaseline)
	header += "   " + formatPaceInfo(m.writing.liveWPM, m.writing.intensityRatio, m.writing.hasBaseline)
	// The header must stay one line: writingChromeLines budgets exactly one
	// for it, so a wrapped header pushes the textarea off the bottom.
	header = truncateToWidth(header, m.width)
	help := statStyle.Render("ctrl+n: new entry   ctrl+t: toggle size   esc: end session")
	view := titleStyle.Render(header) + "\n\n" + m.writing.textarea.View() + "\n\n" + help
	if m.writing.pasteWarning != "" {
		view += "\n" + errorStyle.Render(m.writing.pasteWarning)
	}
	return view
}
