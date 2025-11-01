package styles

import (
	"testing"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/stretchr/testify/assert"
)

func TestThemeSwitching(t *testing.T) {
	// Start with dark theme
	darkTheme := DarkTheme()
	SetTheme(darkTheme)

	// Verify dark theme colors are applied
	assert.Equal(t, lipgloss.Color(darkTheme.TextPrimary), TextPrimary)
	assert.Equal(t, lipgloss.Color(darkTheme.Background), Background)
	assert.Equal(t, lipgloss.Color(darkTheme.Accent), Accent)

	// Switch to light theme
	lightTheme := LightTheme()
	SetTheme(lightTheme)

	// Verify light theme colors are applied
	assert.Equal(t, lipgloss.Color(lightTheme.TextPrimary), TextPrimary)
	assert.Equal(t, lipgloss.Color(lightTheme.Background), Background)
	assert.Equal(t, lipgloss.Color(lightTheme.Accent), Accent)

	// Verify the colors are different between themes
	assert.NotEqual(t, darkTheme.TextPrimary, lightTheme.TextPrimary)
	assert.NotEqual(t, darkTheme.Background, lightTheme.Background)
}

func TestToggleTheme(t *testing.T) {
	// Start with dark theme
	SetTheme(DarkTheme())
	assert.Equal(t, ThemeDark, CurrentTheme.Type)

	// Toggle to light
	theme := ToggleTheme()
	assert.Equal(t, ThemeLight, theme.Type)
	assert.Equal(t, ThemeLight, CurrentTheme.Type)

	// Toggle back to dark
	theme = ToggleTheme()
	assert.Equal(t, ThemeDark, theme.Type)
	assert.Equal(t, ThemeDark, CurrentTheme.Type)
}

func TestLightThemeVisibility(t *testing.T) {
	// Get fresh light theme (don't use CurrentTheme as it may be modified by other tests)
	lightTheme := LightTheme()

	// Verify text colors are dark (visible on light background)
	// Light theme should have dark text colors for visibility
	assert.Equal(t, "#1F2937", lightTheme.TextPrimary)   // Dark gray
	assert.Equal(t, "#4B5563", lightTheme.TextSecondary) // Medium gray
	assert.Equal(t, "#F5F5F5", lightTheme.Background)    // Off-white background

	// Verify the colors are actually different from dark theme
	darkTheme := DarkTheme()
	assert.NotEqual(t, darkTheme.TextPrimary, lightTheme.TextPrimary)
	assert.NotEqual(t, darkTheme.Background, lightTheme.Background)
}
