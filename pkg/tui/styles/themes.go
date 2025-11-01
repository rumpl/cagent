package styles

import (
	"github.com/charmbracelet/lipgloss/v2"
)

// ThemeType represents the type of theme
type ThemeType string

const (
	ThemeDark  ThemeType = "dark"
	ThemeLight ThemeType = "light"
)

// Theme defines a complete color scheme for the TUI
type Theme struct {
	Type ThemeType

	// Background colors (stored as hex strings)
	Background    string
	BackgroundAlt string

	// Primary accent colors
	Accent    string
	AccentDim string

	// Status colors
	Success string
	Error   string
	Warning string
	Info    string

	// Text hierarchy
	TextPrimary   string
	TextSecondary string
	TextMuted     string
	TextSubtle    string

	// Border colors
	BorderPrimary   string
	BorderSecondary string
	BorderMuted     string
	BorderWarning   string
	BorderError     string

	// Diff colors
	DiffAddBg    string
	DiffRemoveBg string
	DiffAddFg    string
	DiffRemoveFg string

	// Interactive element colors
	Selected         string
	SelectedFg       string
	Hover            string
	PlaceholderColor string

	// Markdown/Syntax highlighting
	ChromaText                string
	ChromaError               string
	ChromaErrorBg             string
	ChromaComment             string
	ChromaCommentPreproc      string
	ChromaKeyword             string
	ChromaKeywordReserved     string
	ChromaKeywordNamespace    string
	ChromaKeywordType         string
	ChromaOperator            string
	ChromaPunctuation         string
	ChromaNameBuiltin         string
	ChromaNameTag             string
	ChromaNameAttribute       string
	ChromaNameDecorator       string
	ChromaLiteralNumber       string
	ChromaLiteralString       string
	ChromaLiteralStringEscape string
	ChromaGenericDeleted      string
	ChromaGenericSubheading   string
	ChromaBackground          string
	ChromaSuccess             string

	// ANSI codes for markdown
	ANSIDocumentColor string
	ANSIH1BgColor     string
	ANSIHeadingColor  string
	ANSILinkTextColor string
	ANSIImageColor    string
	ANSICodeTextColor string
}

// CurrentTheme holds the currently active theme
var CurrentTheme = DarkTheme()

// DarkTheme returns the Tokyo Night-inspired dark theme
func DarkTheme() Theme {
	return Theme{
		Type: ThemeDark,

		// Background colors
		Background:    ColorBackground,
		BackgroundAlt: ColorBackgroundAlt,

		// Primary accent colors
		Accent:    ColorAccentBlue,
		AccentDim: ColorMutedBlue,

		// Status colors
		Success: ColorSuccessGreen,
		Error:   ColorErrorRed,
		Warning: ColorWarningYellow,
		Info:    ColorInfoCyan,

		// Text hierarchy
		TextPrimary:   ColorTextPrimary,
		TextSecondary: ColorTextSecondary,
		TextMuted:     ColorMutedBlue,
		TextSubtle:    ColorBorderSecondary,

		// Border colors
		BorderPrimary:   ColorAccentBlue,
		BorderSecondary: ColorBorderSecondary,
		BorderMuted:     ColorBackgroundAlt,
		BorderWarning:   ColorWarningYellow,
		BorderError:     ColorErrorRed,

		// Diff colors
		DiffAddBg:    ColorDiffAddBg,
		DiffRemoveBg: ColorDiffRemoveBg,
		DiffAddFg:    ColorSuccessGreen,
		DiffRemoveFg: ColorErrorRed,

		// Interactive element colors
		Selected:         ColorSelected,
		SelectedFg:       ColorTextPrimary,
		Hover:            ColorHover,
		PlaceholderColor: ColorMutedBlue,

		// Chroma syntax highlighting
		ChromaText:                ColorTextPrimary,
		ChromaError:               ChromaErrorFgColor,
		ChromaErrorBg:             ChromaErrorBgColor,
		ChromaComment:             ChromaCommentColor,
		ChromaCommentPreproc:      ChromaCommentPreprocColor,
		ChromaKeyword:             ChromaKeywordColor,
		ChromaKeywordReserved:     ChromaKeywordReservedColor,
		ChromaKeywordNamespace:    ChromaKeywordNamespaceColor,
		ChromaKeywordType:         ChromaKeywordTypeColor,
		ChromaOperator:            ChromaOperatorColor,
		ChromaPunctuation:         ChromaPunctuationColor,
		ChromaNameBuiltin:         ChromaNameBuiltinColor,
		ChromaNameTag:             ChromaNameTagColor,
		ChromaNameAttribute:       ChromaNameAttributeColor,
		ChromaNameDecorator:       ChromaNameDecoratorColor,
		ChromaLiteralNumber:       ChromaLiteralNumberColor,
		ChromaLiteralString:       ChromaLiteralStringColor,
		ChromaLiteralStringEscape: ChromaLiteralStringEscapeColor,
		ChromaGenericDeleted:      ChromaGenericDeletedColor,
		ChromaGenericSubheading:   ChromaGenericSubheadingColor,
		ChromaBackground:          ChromaBackgroundColor,
		ChromaSuccess:             ChromaSuccessColor,

		// ANSI codes
		ANSIDocumentColor: ANSIColor252,
		ANSIH1BgColor:     ANSIColor63,
		ANSIHeadingColor:  ANSIColor39,
		ANSILinkTextColor: ANSIColor35,
		ANSIImageColor:    ANSIColor212,
		ANSICodeTextColor: ANSIColor244,
	}
}

// LightTheme returns a light theme suitable for bright environments
func LightTheme() Theme {
	return Theme{
		Type: ThemeLight,

		// Background colors
		Background:    "#F5F5F5",
		BackgroundAlt: "#F5F5F5",

		// Primary accent colors
		Accent:    "#3B82F6", // Blue
		AccentDim: "#6B7280", // Gray

		// Status colors
		Success: "#10B981", // Green
		Error:   "#EF4444", // Red
		Warning: "#F59E0B", // Amber
		Info:    "#06B6D4", // Cyan

		// Text hierarchy
		TextPrimary:   "#1F2937", // Dark gray
		TextSecondary: "#4B5563", // Medium gray
		TextMuted:     "#6B7280", // Light gray
		TextSubtle:    "#9CA3AF", // Very light gray

		// Border colors
		BorderPrimary:   "#3B82F6", // Blue
		BorderSecondary: "#D1D5DB", // Light gray
		BorderMuted:     "#E5E7EB", // Very light gray
		BorderWarning:   "#F59E0B", // Amber
		BorderError:     "#EF4444", // Red

		// Diff colors
		DiffAddBg:    "#DCFCE7", // Light green
		DiffRemoveBg: "#FEE2E2", // Light red
		DiffAddFg:    "#166534", // Dark green
		DiffRemoveFg: "#991B1B", // Dark red

		// Interactive element colors
		Selected:         "#DBEAFE", // Light blue
		SelectedFg:       "#1E40AF", // Dark blue
		Hover:            "#EFF6FF", // Very light blue
		PlaceholderColor: "#9CA3AF", // Light gray

		// Chroma syntax highlighting (GitHub light theme inspired)
		ChromaText:                "#24292E",
		ChromaError:               "#FFFFFF",
		ChromaErrorBg:             "#D73A49",
		ChromaComment:             "#6A737D",
		ChromaCommentPreproc:      "#D73A49",
		ChromaKeyword:             "#D73A49",
		ChromaKeywordReserved:     "#D73A49",
		ChromaKeywordNamespace:    "#D73A49",
		ChromaKeywordType:         "#D73A49",
		ChromaOperator:            "#D73A49",
		ChromaPunctuation:         "#24292E",
		ChromaNameBuiltin:         "#005CC5",
		ChromaNameTag:             "#22863A",
		ChromaNameAttribute:       "#6F42C1",
		ChromaNameDecorator:       "#6F42C1",
		ChromaLiteralNumber:       "#005CC5",
		ChromaLiteralString:       "#032F62",
		ChromaLiteralStringEscape: "#032F62",
		ChromaGenericDeleted:      "#B31D28",
		ChromaGenericSubheading:   "#6A737D",
		ChromaBackground:          "#FFFFFF",
		ChromaSuccess:             "#28A745",

		// ANSI codes
		ANSIDocumentColor: "16",  // Black
		ANSIH1BgColor:     "27",  // Blue background
		ANSIHeadingColor:  "21",  // Blue
		ANSILinkTextColor: "27",  // Blue
		ANSIImageColor:    "133", // Purple
		ANSICodeTextColor: "240", // Gray
	}
}

// SetTheme updates the current theme and refreshes all styles
func SetTheme(theme Theme) {
	CurrentTheme = theme

	// Update global style variables
	Background = lipgloss.Color(theme.Background)
	BackgroundAlt = lipgloss.Color(theme.BackgroundAlt)

	Accent = lipgloss.Color(theme.Accent)
	AccentDim = lipgloss.Color(theme.AccentDim)

	Success = lipgloss.Color(theme.Success)
	Error = lipgloss.Color(theme.Error)
	Warning = lipgloss.Color(theme.Warning)
	Info = lipgloss.Color(theme.Info)

	TextPrimary = lipgloss.Color(theme.TextPrimary)
	TextSecondary = lipgloss.Color(theme.TextSecondary)
	TextMuted = lipgloss.Color(theme.TextMuted)
	TextSubtle = lipgloss.Color(theme.TextSubtle)

	BorderPrimary = lipgloss.Color(theme.BorderPrimary)
	BorderSecondary = lipgloss.Color(theme.BorderSecondary)
	BorderMuted = lipgloss.Color(theme.BorderMuted)
	BorderWarning = lipgloss.Color(theme.BorderWarning)
	BorderError = lipgloss.Color(theme.BorderError)

	DiffAddBg = lipgloss.Color(theme.DiffAddBg)
	DiffRemoveBg = lipgloss.Color(theme.DiffRemoveBg)
	DiffAddFg = lipgloss.Color(theme.DiffAddFg)
	DiffRemoveFg = lipgloss.Color(theme.DiffRemoveFg)

	Selected = lipgloss.Color(theme.Selected)
	SelectedFg = lipgloss.Color(theme.SelectedFg)
	Hover = lipgloss.Color(theme.Hover)
	PlaceholderColor = lipgloss.Color(theme.PlaceholderColor)

	// Rebuild all styles with new theme colors
	rebuildStyles()
}

// rebuildStyles recreates all style objects with the current theme
func rebuildStyles() {
	// Base Styles
	BaseStyle = lipgloss.NewStyle().Foreground(TextPrimary)
	AppStyle = BaseStyle.Padding(0, 1, 0, 1)

	// Text Styles
	HighlightStyle = BaseStyle.Foreground(Accent)
	MutedStyle = BaseStyle.Foreground(TextMuted)
	SubtleStyle = BaseStyle.Foreground(TextSubtle)
	SecondaryStyle = BaseStyle.Foreground(TextSecondary)
	BoldStyle = BaseStyle.Bold(true)
	ItalicStyle = BaseStyle.Italic(true)

	// Status Styles
	SuccessStyle = BaseStyle.Foreground(Success)
	ErrorStyle = BaseStyle.Foreground(Error)
	WarningStyle = BaseStyle.Foreground(Warning)
	InfoStyle = BaseStyle.Foreground(Info)
	ActiveStyle = BaseStyle.Foreground(Success)
	InProgressStyle = BaseStyle.Foreground(Warning)
	PendingStyle = BaseStyle.Foreground(TextSecondary)

	// Layout Styles
	HeaderStyle = BaseStyle.Foreground(Accent).Padding(0, 0, 1, 0)
	PaddedContentStyle = BaseStyle.Padding(1, 2)
	CenterStyle = BaseStyle.Align(lipgloss.Center, lipgloss.Center)

	// Border Styles
	BorderStyle = BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(BorderPrimary)

	BorderedBoxStyle = BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(BorderSecondary).
		Padding(0, 1)

	BorderedBoxFocusedStyle = BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(BorderPrimary).
		Padding(0, 1)

	UserMessageBorderStyle = BaseStyle.
		PaddingLeft(1).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(BorderPrimary)

	// Dialog Styles
	DialogStyle = BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(BorderSecondary).
		Foreground(TextPrimary).
		Padding(1, 2).
		Align(lipgloss.Left)

	DialogWarningStyle = BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(BorderWarning).
		Foreground(TextPrimary).
		Padding(1, 2).
		Align(lipgloss.Left)

	DialogTitleStyle = BaseStyle.
		Bold(true).
		Foreground(TextSecondary).
		Align(lipgloss.Center)

	DialogTitleWarningStyle = BaseStyle.
		Bold(true).
		Foreground(Warning).
		Align(lipgloss.Center)

	DialogTitleInfoStyle = BaseStyle.
		Bold(true).
		Foreground(Info).
		Align(lipgloss.Center)

	DialogContentStyle = BaseStyle.Foreground(TextPrimary)
	DialogSeparatorStyle = BaseStyle.Foreground(BorderMuted)

	DialogLabelStyle = BaseStyle.
		Bold(true).
		Foreground(TextMuted)

	DialogValueStyle = BaseStyle.
		Bold(true).
		Foreground(TextSecondary)

	DialogQuestionStyle = BaseStyle.
		Bold(true).
		Foreground(TextPrimary).
		Align(lipgloss.Center)

	DialogOptionsStyle = BaseStyle.
		Foreground(TextMuted).
		Align(lipgloss.Center)

	DialogHelpStyle = BaseStyle.
		Foreground(TextMuted).
		Italic(true)

	// Command Palette Styles
	PaletteSelectedStyle = BaseStyle.
		Background(Selected).
		Foreground(SelectedFg).
		Padding(0, 1)

	PaletteUnselectedStyle = BaseStyle.
		Foreground(TextPrimary).
		Padding(0, 1)

	PaletteCategoryStyle = BaseStyle.
		Bold(true).
		Foreground(TextMuted).
		MarginTop(1)

	PaletteDescStyle = BaseStyle.Foreground(TextMuted)

	// Diff Styles
	DiffAddStyle = BaseStyle.
		Background(DiffAddBg).
		Foreground(DiffAddFg)

	DiffRemoveStyle = BaseStyle.
		Background(DiffRemoveBg).
		Foreground(DiffRemoveFg)

	DiffUnchangedStyle = lipgloss.NewStyle()
	DiffContextStyle = BaseStyle

	// Tool Call Styles
	ToolCallArgs = BaseStyle.
		PaddingLeft(1).
		BorderLeft(true).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(BorderSecondary)

	ToolCallArgKey = BaseStyle.Bold(true).Foreground(TextSecondary)

	ToolCallResult = BaseStyle.
		PaddingLeft(1).
		BorderLeft(true).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(BorderSecondary)

	ToolCallResultKey = BaseStyle.Bold(true).Foreground(TextSecondary)

	// Input Styles
	InputStyle.Focused.Base = BaseStyle
	InputStyle.Focused.Placeholder = BaseStyle.Foreground(PlaceholderColor)
	InputStyle.Blurred.Base = BaseStyle
	InputStyle.Blurred.Placeholder = BaseStyle.Foreground(PlaceholderColor)
	InputStyle.Cursor.Color = Accent

	EditorStyle = BaseStyle.Padding(2, 0, 0, 0)

	// Notification Styles
	NotificationStyle = BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Success).
		Padding(0, 1)

	NotificationInfoStyle = BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Info).
		Padding(0, 1)

	NotificationWarningStyle = BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Warning).
		Padding(0, 1)

	NotificationErrorStyle = BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Error).
		Padding(0, 1)

	// Completion Styles
	CompletionBoxStyle = BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(BorderSecondary).
		Padding(0, 1)

	CompletionSelectedStyle = BaseStyle.
		Foreground(TextPrimary).
		Bold(true)

	CompletionNormalStyle = BaseStyle.Foreground(TextPrimary)

	CompletionDescStyle = BaseStyle.
		Foreground(TextSecondary).
		Italic(true)

	CompletionNoResultsStyle = BaseStyle.
		Foreground(TextMuted).
		Italic(true).
		Align(lipgloss.Center)

	// Deprecated styles
	StatusStyle = MutedStyle
	ActionStyle = SecondaryStyle
	ChatStyle = BaseStyle

	// Selection Styles
	SelectionStyle = BaseStyle.
		Background(Selected).
		Foreground(SelectedFg)
}

// ToggleTheme switches between light and dark themes
func ToggleTheme() Theme {
	if CurrentTheme.Type == ThemeDark {
		SetTheme(LightTheme())
	} else {
		SetTheme(DarkTheme())
	}
	return CurrentTheme
}

// GetTheme returns the current theme
func GetTheme() Theme {
	return CurrentTheme
}
