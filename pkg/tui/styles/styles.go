package styles

import (
	"image/color"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/bubbles/v2/textarea"
	"github.com/charmbracelet/glamour/v2/ansi"
	"github.com/charmbracelet/lipgloss/v2"
)

const (
	defaultListIndent = 2
	defaultMargin     = 2
)

// Color hex values (used throughout the file)
const (
	// Primary colors
	ColorAccentBlue      = "#7AA2F7" // Soft blue
	ColorMutedBlue       = "#565F89" // Dark blue-grey
	ColorBackgroundAlt   = "#24283B" // Slightly lighter background
	ColorBorderSecondary = "#414868" // Dark blue-grey
	ColorTextPrimary     = "#C0CAF5" // Light blue-white
	ColorTextSecondary   = "#9AA5CE" // Medium blue-grey
	ColorSuccessGreen    = "#9ECE6A" // Soft green
	ColorErrorRed        = "#F7768E" // Soft red
	ColorWarningYellow   = "#E0AF68" // Soft yellow

	// Background colors
	ColorBackground = "#1A1B26" // Dark blue-black

	// Status colors
	ColorInfoCyan = "#7DCFFF" // Soft cyan

	// Diff colors
	ColorDiffAddBg    = "#20303B" // Dark blue-green
	ColorDiffRemoveBg = "#3C2A2A" // Dark red-brown

	// Interactive element colors
	ColorSelected = "#364A82" // Dark blue for selected items
	ColorHover    = "#2D3F5F" // Slightly lighter than selected
)

// Chroma syntax highlighting colors (Monokai theme)
const (
	ChromaErrorFgColor             = "#F1F1F1"
	ChromaSuccessColor             = "#00D787"
	ChromaErrorBgColor             = "#F05B5B"
	ChromaCommentColor             = "#676767"
	ChromaCommentPreprocColor      = "#FF875F"
	ChromaKeywordColor             = "#00AAFF"
	ChromaKeywordReservedColor     = "#FF5FD2"
	ChromaKeywordNamespaceColor    = "#FF5F87"
	ChromaKeywordTypeColor         = "#6E6ED8"
	ChromaOperatorColor            = "#EF8080"
	ChromaPunctuationColor         = "#E8E8A8"
	ChromaNameBuiltinColor         = "#FF8EC7"
	ChromaNameTagColor             = "#B083EA"
	ChromaNameAttributeColor       = "#7A7AE6"
	ChromaNameDecoratorColor       = "#FFFF87"
	ChromaLiteralNumberColor       = "#6EEFC0"
	ChromaLiteralStringColor       = "#C69669"
	ChromaLiteralStringEscapeColor = "#AFFFD7"
	ChromaGenericDeletedColor      = "#FD5B5B"
	ChromaGenericSubheadingColor   = "#777777"
	ChromaBackgroundColor          = "#373737"
)

// ANSI color codes (8-bit color codes)
const (
	ANSIColor252 = "252"
	ANSIColor39  = "39"
	ANSIColor63  = "63"
	ANSIColor35  = "35"
	ANSIColor212 = "212"
	ANSIColor243 = "243"
	ANSIColor244 = "244"
)

// Tokyo Night-inspired Color Palette
// These variables are updated by SetTheme() when the theme changes
var (
	// Background colors
	Background    color.Color
	BackgroundAlt color.Color

	// Primary accent colors
	Accent    color.Color
	AccentDim color.Color

	// Status colors - softer, more professional
	Success color.Color
	Error   color.Color
	Warning color.Color
	Info    color.Color

	// Text hierarchy
	TextPrimary   color.Color
	TextSecondary color.Color
	TextMuted     color.Color
	TextSubtle    color.Color

	// Border colors
	BorderPrimary   color.Color
	BorderSecondary color.Color
	BorderMuted     color.Color
	BorderWarning   color.Color
	BorderError     color.Color

	// Diff colors (matching glamour/markdown "dark" theme)
	DiffAddBg    color.Color
	DiffRemoveBg color.Color
	DiffAddFg    color.Color
	DiffRemoveFg color.Color

	// Interactive element colors
	Selected         color.Color
	SelectedFg       color.Color
	Hover            color.Color
	PlaceholderColor color.Color
)

func init() {
	// Initialize with dark theme colors
	theme := DarkTheme()
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
}

// Base Styles
var (
	BaseStyle = lipgloss.NewStyle().Foreground(TextPrimary)
	AppStyle  = BaseStyle.Padding(0, 1, 0, 1)
)

// Text Styles
var (
	HighlightStyle = BaseStyle.Foreground(Accent)
	MutedStyle     = BaseStyle.Foreground(TextMuted)
	SubtleStyle    = BaseStyle.Foreground(TextSubtle)
	SecondaryStyle = BaseStyle.Foreground(TextSecondary)
	BoldStyle      = BaseStyle.Bold(true)
	ItalicStyle    = BaseStyle.Italic(true)
)

// Status Styles
var (
	SuccessStyle    = BaseStyle.Foreground(Success)
	ErrorStyle      = BaseStyle.Foreground(Error)
	WarningStyle    = BaseStyle.Foreground(Warning)
	InfoStyle       = BaseStyle.Foreground(Info)
	ActiveStyle     = BaseStyle.Foreground(Success)
	InProgressStyle = BaseStyle.Foreground(Warning)
	PendingStyle    = BaseStyle.Foreground(TextSecondary)
)

// Layout Styles
var (
	HeaderStyle        = BaseStyle.Foreground(Accent).Padding(0, 0, 1, 0)
	PaddedContentStyle = BaseStyle.Padding(1, 2)
	CenterStyle        = BaseStyle.Align(lipgloss.Center, lipgloss.Center)
)

// Border Styles
var (
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
)

// Dialog Styles
var (
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

	DialogContentStyle = BaseStyle.
				Foreground(TextPrimary)

	DialogSeparatorStyle = BaseStyle.
				Foreground(BorderMuted)

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
)

// Command Palette Styles
var (
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

	PaletteDescStyle = BaseStyle.
				Foreground(TextMuted)
)

// Diff Styles (matching glamour markdown theme)
var (
	DiffAddStyle = BaseStyle.
			Background(DiffAddBg).
			Foreground(DiffAddFg)

	DiffRemoveStyle = BaseStyle.
			Background(DiffRemoveBg).
			Foreground(DiffRemoveFg)

	DiffUnchangedStyle = lipgloss.NewStyle()

	DiffContextStyle = BaseStyle
)

// Tool Call Styles
var (
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
)

// Input Styles
var (
	InputStyle = textarea.Styles{
		Focused: textarea.StyleState{
			Base:        BaseStyle,
			Placeholder: BaseStyle.Foreground(PlaceholderColor),
		},
		Blurred: textarea.StyleState{
			Base:        BaseStyle,
			Placeholder: BaseStyle.Foreground(PlaceholderColor),
		},
		Cursor: textarea.CursorStyle{
			Color: Accent,
		},
	}
	EditorStyle = BaseStyle.Padding(2, 0, 0, 0)
)

// Notification Styles
var (
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
)

// Completion Styles
var (
	CompletionBoxStyle = BaseStyle.
				Border(lipgloss.RoundedBorder()).
				BorderForeground(BorderSecondary).
				Padding(0, 1)

	CompletionSelectedStyle = BaseStyle.
				Foreground(TextPrimary).
				Bold(true)

	CompletionNormalStyle = BaseStyle.
				Foreground(TextPrimary)

	CompletionDescStyle = BaseStyle.
				Foreground(TextSecondary).
				Italic(true)

	CompletionNoResultsStyle = BaseStyle.
					Foreground(TextMuted).
					Italic(true).
					Align(lipgloss.Center)
)

// Deprecated styles (kept for backward compatibility)
var (
	StatusStyle = MutedStyle
	ActionStyle = SecondaryStyle
	ChatStyle   = BaseStyle
)

// Selection Styles
var (
	SelectionStyle = BaseStyle.
		Background(Selected).
		Foreground(SelectedFg)
)

func toChroma(style ansi.StylePrimitive) string {
	var s []string

	if style.Color != nil {
		s = append(s, *style.Color)
	}
	if style.BackgroundColor != nil {
		s = append(s, "bg:"+*style.BackgroundColor)
	}
	if style.Italic != nil && *style.Italic {
		s = append(s, "italic")
	}
	if style.Bold != nil && *style.Bold {
		s = append(s, "bold")
	}
	if style.Underline != nil && *style.Underline {
		s = append(s, "underline")
	}

	return strings.Join(s, " ")
}

func getChromaTheme() chroma.StyleEntries {
	md := MarkdownStyle().CodeBlock
	_ = CurrentTheme // Use theme variable to avoid unused warning
	return chroma.StyleEntries{
		chroma.Text:                toChroma(md.Chroma.Text),
		chroma.Error:               toChroma(md.Chroma.Error),
		chroma.Comment:             toChroma(md.Chroma.Comment),
		chroma.CommentPreproc:      toChroma(md.Chroma.CommentPreproc),
		chroma.Keyword:             toChroma(md.Chroma.Keyword),
		chroma.KeywordReserved:     toChroma(md.Chroma.KeywordReserved),
		chroma.KeywordNamespace:    toChroma(md.Chroma.KeywordNamespace),
		chroma.KeywordType:         toChroma(md.Chroma.KeywordType),
		chroma.Operator:            toChroma(md.Chroma.Operator),
		chroma.Punctuation:         toChroma(md.Chroma.Punctuation),
		chroma.Name:                toChroma(md.Chroma.Name),
		chroma.NameBuiltin:         toChroma(md.Chroma.NameBuiltin),
		chroma.NameTag:             toChroma(md.Chroma.NameTag),
		chroma.NameAttribute:       toChroma(md.Chroma.NameAttribute),
		chroma.NameClass:           toChroma(md.Chroma.NameClass),
		chroma.NameDecorator:       toChroma(md.Chroma.NameDecorator),
		chroma.NameFunction:        toChroma(md.Chroma.NameFunction),
		chroma.LiteralNumber:       toChroma(md.Chroma.LiteralNumber),
		chroma.LiteralString:       toChroma(md.Chroma.LiteralString),
		chroma.LiteralStringEscape: toChroma(md.Chroma.LiteralStringEscape),
		chroma.GenericDeleted:      toChroma(md.Chroma.GenericDeleted),
		chroma.GenericEmph:         toChroma(md.Chroma.GenericEmph),
		chroma.GenericInserted:     toChroma(md.Chroma.GenericInserted),
		chroma.GenericStrong:       toChroma(md.Chroma.GenericStrong),
		chroma.GenericSubheading:   toChroma(md.Chroma.GenericSubheading),
		chroma.Background:          toChroma(md.Chroma.Background),
	}
}

func ChromaStyle() *chroma.Style {
	style, err := chroma.NewStyle("cagent", getChromaTheme())
	if err != nil {
		panic(err)
	}
	return style
}

func MarkdownStyle() ansi.StyleConfig {
	theme := CurrentTheme

	h1Color := theme.Accent
	h2Color := theme.Accent
	h3Color := theme.TextSecondary
	h4Color := theme.TextSecondary
	h5Color := theme.TextSecondary
	h6Color := theme.TextMuted
	linkColor := theme.Accent
	strongColor := theme.TextPrimary
	codeColor := theme.TextPrimary
	codeBgColor := theme.BackgroundAlt
	blockquoteColor := theme.TextSecondary
	listColor := theme.TextPrimary
	hrColor := theme.BorderSecondary
	codeBg := theme.BackgroundAlt

	customDarkStyle := ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockPrefix: "",
				BlockSuffix: "",
				Color:       stringPtr(theme.ANSIDocumentColor),
			},
			Margin: uintPtr(0),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: &blockquoteColor,
			},
			Indent:      uintPtr(1),
			IndentToken: nil,
		},
		List: ansi.StyleList{
			LevelIndent: defaultListIndent,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       stringPtr(theme.ANSIHeadingColor),
				Bold:        boolPtr(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           &h1Color,
				BackgroundColor: stringPtr(theme.ANSIH1BgColor),
				Bold:            boolPtr(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
				Color:  &h2Color,
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
				Color:  &h3Color,
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
				Color:  &h4Color,
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "##### ",
				Color:  &h5Color,
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Color:  &h6Color,
				Bold:   boolPtr(false),
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: boolPtr(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: boolPtr(true),
		},
		Strong: ansi.StylePrimitive{
			Color: &strongColor,
			Bold:  boolPtr(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  &hrColor,
			Format: "\n--------\n",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			StylePrimitive: ansi.StylePrimitive{},
			Ticked:         "[✓] ",
			Unticked:       "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     &linkColor,
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: stringPtr(theme.ANSILinkTextColor),
			Bold:  boolPtr(true),
		},
		Image: ansi.StylePrimitive{
			Color:     stringPtr(theme.ANSIImageColor),
			Underline: boolPtr(true),
		},
		ImageText: ansi.StylePrimitive{
			Color:  stringPtr(theme.ANSICodeTextColor),
			Format: "Image: {{.text}} →",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           &codeColor,
				BackgroundColor: &codeBgColor,
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: stringPtr(theme.ANSICodeTextColor),
				},
				Margin: uintPtr(defaultMargin),
			},
			Theme: "cagent",
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaText),
				},
				Error: ansi.StylePrimitive{
					Color:           stringPtr(theme.ChromaError),
					BackgroundColor: stringPtr(theme.ChromaErrorBg),
				},
				Comment: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaComment),
				},
				CommentPreproc: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaCommentPreproc),
				},
				Keyword: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaKeyword),
				},
				KeywordReserved: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaKeywordReserved),
				},
				KeywordNamespace: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaKeywordNamespace),
				},
				KeywordType: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaKeywordType),
				},
				Operator: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaOperator),
				},
				Punctuation: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaPunctuation),
				},
				Name: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaText),
				},
				NameBuiltin: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaNameBuiltin),
				},
				NameTag: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaNameTag),
				},
				NameAttribute: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaNameAttribute),
				},
				NameClass: ansi.StylePrimitive{
					Color:     stringPtr(theme.ChromaError),
					Underline: boolPtr(true),
					Bold:      boolPtr(true),
				},
				NameDecorator: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaNameDecorator),
				},
				NameFunction: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaSuccess),
				},
				LiteralNumber: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaLiteralNumber),
				},
				LiteralString: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaLiteralString),
				},
				LiteralStringEscape: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaLiteralStringEscape),
				},
				GenericDeleted: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaGenericDeleted),
				},
				GenericEmph: ansi.StylePrimitive{
					Italic: boolPtr(true),
				},
				GenericInserted: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaSuccess),
				},
				GenericStrong: ansi.StylePrimitive{
					Bold: boolPtr(true),
				},
				GenericSubheading: ansi.StylePrimitive{
					Color: stringPtr(theme.ChromaGenericSubheading),
				},
				Background: ansi.StylePrimitive{
					BackgroundColor: stringPtr(theme.ChromaBackground),
				},
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "\n🠶 ",
		},
	}

	customDarkStyle.List.Color = &listColor
	customDarkStyle.CodeBlock.BackgroundColor = &codeBg

	return customDarkStyle
}

func uintPtr(u uint) *uint {
	return &u
}

func boolPtr(b bool) *bool {
	return &b
}

func stringPtr(s string) *string {
	return &s
}
