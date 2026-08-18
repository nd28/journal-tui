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

	// savedDraft is what the buffer held at the last autosave. Comparing
	// against it keeps the periodic save from rewriting an unchanged draft
	// every few seconds while the writer sits and thinks.
	savedDraft string

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

// autosaveTickMsg carries the session it was armed for. A tick that outlives
// its session (ended, then a new one started inside the same interval) would
// otherwise land on the writing screen and re-arm itself there, leaving two
// tickers running against one session — and one more for every restart.
type autosaveTickMsg struct {
	sessionID int64
}

// autosaveInterval is how often the in-progress buffer is mirrored to disk.
// Nothing else writes text until the session ends, so this interval is
// exactly how much writing a power cut can take — a sentence, not a session.
// Long enough that a steady typist causes one small write per interval
// rather than one per keystroke.
const autosaveInterval = 5 * time.Second

func autosaveTick(sessionID int64) tea.Cmd {
	return tea.Tick(autosaveInterval, func(time.Time) tea.Msg {
		return autosaveTickMsg{sessionID: sessionID}
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

	ta := newWritingTextarea(m.width, m.height, m.compactMode)
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
	return m, tea.Batch(focusCmd, comboTick(), autosaveTick(sessionID))
}

// newWritingTextarea builds the editor for a writing session, sized for the
// current terminal. Shared by a fresh session and a resumed one so both get
// the same paste guards and the same line ceiling.
func newWritingTextarea(termWidth, termHeight int, compact bool) textarea.Model {
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
	// MaxHeight defaults to 99, and bubbles enforces it by silently swallowing
	// the enter key once the buffer holds that many lines — a long session
	// hits an invisible wall mid-thought with no way to tell why. Zero lifts
	// the cap to bubbles' own 10,000-line ceiling, which is the real limit
	// either way since SetValue never consulted MaxHeight.
	ta.MaxHeight = 0
	w, h := writingDimensions(termWidth, termHeight, compact)
	ta.SetWidth(w)
	ta.SetHeight(h)
	return ta
}

// resumeWritingSession reopens a session the app was killed in the middle of:
// the autosaved text goes back in the editor and the running score goes back
// on the header, so an interrupted session continues instead of restarting.
func (m Model) resumeWritingSession(u store.UnfinishedSession) (tea.Model, tea.Cmd) {
	now := time.Now()
	// The streak belongs to the day the writing is finished on — today — not
	// to the day the interrupted session happened to start.
	today := now.Format("2006-01-02")
	newStreak := store.ComputeStreak(m.stats.LastEntryDate, today, m.stats.CurrentStreak)

	draft, hasDraft, err := m.store.GetDraft(u.ID)
	if err != nil {
		m.err = err
		return m, nil
	}

	baselineWPM, hasBaseline, err := m.store.RecentAvgPace(baselinePaceSessionWindow)
	if err != nil {
		m.err = err
		return m, nil
	}

	priorWords, priorPoints := draft.TotalWords, draft.RawScore
	if !hasDraft {
		// A session interrupted before autosave existed, or before its first
		// tick, has entries on disk but no recorded score. Credit those words
		// at the base rate: it is the least they can have earned, and a
		// header reading "Words: 50   Score: 0" looks like a bug rather than
		// like history.
		priorWords = u.SavedWords
		priorPoints = u.SavedWords * scoring.BasePointsPerWord
	}

	ta := newWritingTextarea(m.width, m.height, m.compactMode)
	if draft.Body != "" {
		ta.SetValue(draft.Body)
	}
	focusCmd := ta.Focus()
	// bubbles only scrolls its viewport to the cursor at the end of Update,
	// so without one the recovered text renders from the top while the cursor
	// sits at the bottom. An empty update settles the view before first paint.
	ta, _ = ta.Update(nil)

	m.writing = writingState{
		textarea: ta,
		session:  scoring.RestoreSession(now, priorWords, priorPoints),
		// The recovered text was already scored once. Without seeding the
		// high-water mark, every word of it would be paid for a second time.
		lastWordCount: completedWords(draft.Body),
		sessionID:     u.ID,
		startedAt:     now,
		streakDays:    newStreak,
		entryDate:     today,
		baselineWPM:   baselineWPM,
		hasBaseline:   hasBaseline,
		savedDraft:    draft.Body,
	}
	m.recovery = nil
	m.recoveryCount = 0
	m.homeCursor = 0
	m.screen = screenWriting
	return m, tea.Batch(focusCmd, comboTick(), autosaveTick(u.ID))
}

// saveDraft mirrors the in-progress buffer and the running score to disk.
// This is the only thing standing between a hard kill and a lost session:
// entry rows are written only when the writer asks for a new entry or ends
// the session, and a power cut asks for neither.
func (m *Model) saveDraft() {
	body := m.writing.textarea.Value()
	if err := m.store.SaveDraft(
		m.writing.sessionID,
		time.Now(),
		body,
		m.writing.session.RawScore(),
		m.writing.session.TotalWords(),
	); err != nil {
		m.err = err
		return
	}
	m.writing.savedDraft = body
}

// refreshRecovery reloads which interrupted session, if any, is waiting to be
// picked up. Called at startup and whenever a session ends, so finishing one
// recovered session surfaces the next instead of hiding it until restart.
func (m *Model) refreshRecovery() {
	sessions, err := m.store.UnfinishedSessions()
	if err != nil {
		m.err = err
		return
	}
	m.recoveryCount = len(sessions)
	if len(sessions) == 0 {
		m.recovery = nil
		return
	}
	newest := sessions[0]
	m.recovery = &newest
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

	// The draft exists only to survive a crash. Its text now lives in an
	// entry row, so leaving it behind would offer a finished session back as
	// recoverable.
	if err := m.store.DeleteDraft(m.writing.sessionID); err != nil {
		m.err = err
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
		m.refreshRecovery()
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
	m.refreshRecovery()
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

	if tick, ok := msg.(autosaveTickMsg); ok {
		if tick.sessionID != m.writing.sessionID {
			// Left over from a session that already ended. Let it stop here
			// instead of re-arming against a session it doesn't belong to.
			return m, nil
		}
		if m.writing.textarea.Value() != m.writing.savedDraft {
			m.saveDraft()
		}
		return m, autosaveTick(tick.sessionID)
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
			// This used to quit outright, dropping everything typed since
			// the last new-entry keypress and leaving the session unfinished
			// — so its writing never reached History and its score never
			// counted. End it properly, then quit.
			updated, _ := m.endWritingSession()
			return updated, tea.Quit
		case "esc", "ctrl+d":
			return m.endWritingSession()
		case "ctrl+n":
			if body, words, ok := m.writing.finalizeCurrentEntry(); ok {
				if err := m.store.SaveEntry(m.writing.sessionID, time.Now(), body, words); err != nil {
					m.err = err
				}
			}
			// The buffer is empty again but the score isn't. Rewrite the
			// draft so a crash in the next few seconds restores the running
			// total instead of double-counting the entry just saved.
			m.saveDraft()
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

// writingSaveStatus reports whether what is on screen has reached disk yet.
// The point of autosaving is that the writer never has to wonder, so the
// answer belongs on screen rather than being left to trust.
func writingSaveStatus(buffer, saved string) string {
	if buffer == saved {
		return "saved"
	}
	return "unsaved"
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
	// The help line is truncated like the header: writingChromeLines budgets
	// exactly one line for it, and a wrapped one pushes the textarea off the
	// bottom of the screen.
	helpText := "ctrl+n: new entry   ctrl+t: toggle size   esc: end session   ·   " +
		writingSaveStatus(m.writing.textarea.Value(), m.writing.savedDraft)
	help := statStyle.Render(truncateToWidth(helpText, m.width))
	view := titleStyle.Render(header) + "\n\n" + m.writing.textarea.View() + "\n\n" + help
	if m.writing.pasteWarning != "" {
		view += "\n" + errorStyle.Render(m.writing.pasteWarning)
	}
	return view
}
