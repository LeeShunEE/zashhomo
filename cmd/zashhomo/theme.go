package main

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ColorPalette defines semantic colors for the theme.
type ColorPalette struct {
	Primary   lipgloss.TerminalColor // Main color (titles, emphasis)
	Secondary lipgloss.TerminalColor // Secondary color (labels)
	Accent    lipgloss.TerminalColor // Highlight color (selected items)
	Success   lipgloss.TerminalColor // Success status (running)
	Warning   lipgloss.TerminalColor // Warning status (stopped)
	Muted     lipgloss.TerminalColor // Muted (disabled items, hints)
	Border    lipgloss.TerminalColor // Border color
	Background lipgloss.TerminalColor // Background fill
}

// DefaultPalette returns the default dark theme palette.
func DefaultPalette() ColorPalette {
	return ColorPalette{
		Primary:    lipgloss.Color("13"), // Pink/purple (maintain existing style)
		Secondary:  lipgloss.Color("8"),  // Bright gray
		Accent:     lipgloss.Color("14"), // Bright cyan
		Success:    lipgloss.Color("10"), // Green
		Warning:    lipgloss.Color("11"), // Yellow
		Muted:      lipgloss.Color("8"),  // Dark gray
		Border:     lipgloss.Color("8"),  // Border gray
		Background: lipgloss.Color("235"), // Dark background (optional)
	}
}

// LightPalette returns a palette optimized for light terminal backgrounds.
func LightPalette() ColorPalette {
	return ColorPalette{
		Primary:    lipgloss.Color("5"),  // Magenta (visible on light bg)
		Secondary:  lipgloss.Color("0"),  // Black
		Accent:     lipgloss.Color("6"),  // Cyan
		Success:    lipgloss.Color("2"),  // Green
		Warning:    lipgloss.Color("3"),  // Yellow
		Muted:      lipgloss.Color("7"),  // Light gray
		Border:     lipgloss.Color("7"),  // Border gray
		Background: lipgloss.Color("255"), // White background
	}
}

// Theme contains complete style definitions.
type Theme struct {
	Palette ColorPalette

	// Style components
	Banner     lipgloss.Style
	Title      lipgloss.Style
	Header     lipgloss.Style
	Selected   lipgloss.Style
	Disabled   lipgloss.Style
	Hint       lipgloss.Style
	Label      lipgloss.Style
	StatusOk   lipgloss.Style
	StatusWarn lipgloss.Style
	MenuItem   lipgloss.Style
	Card       lipgloss.Style

	// Output styles for command results
	OutputLabel lipgloss.Style
	OutputValue lipgloss.Style
	OutputTitle lipgloss.Style
}

// DefaultTheme returns the default theme.
func DefaultTheme() Theme {
	p := DefaultPalette()
	return Theme{
		Palette: p,
		Banner: lipgloss.NewStyle().
			Bold(true).
			Foreground(p.Primary),
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(p.Primary).
			Padding(0, 1),
		Header: lipgloss.NewStyle().
			Foreground(p.Secondary),
		Selected: lipgloss.NewStyle().
			Bold(true).
			Foreground(p.Accent),
		Disabled: lipgloss.NewStyle().
			Foreground(p.Muted).
			Faint(true),
		Hint: lipgloss.NewStyle().
			Foreground(p.Muted).
			Faint(true),
		Label: lipgloss.NewStyle().
			Foreground(p.Secondary).
			Width(8),
		StatusOk: lipgloss.NewStyle().
			Foreground(p.Success).
			Bold(true),
		StatusWarn: lipgloss.NewStyle().
			Foreground(p.Warning),
		MenuItem: lipgloss.NewStyle().
			PaddingLeft(2),
		Card: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Border).
			Padding(0, 1).
			Margin(1, 0),
		// Output styles
		OutputLabel: lipgloss.NewStyle().
			Foreground(p.Secondary).
			Width(10).
			Bold(true),
		OutputValue: lipgloss.NewStyle().
			Foreground(p.Primary),
		OutputTitle: lipgloss.NewStyle().
			Foreground(p.Primary).
			Bold(true).
			MarginBottom(1),
	}
}

// LightTheme returns a theme optimized for light terminals.
func LightTheme() Theme {
	p := LightPalette()
	return Theme{
		Palette: p,
		Banner: lipgloss.NewStyle().
			Bold(true).
			Foreground(p.Primary),
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(p.Primary).
			Padding(0, 1),
		Header: lipgloss.NewStyle().
			Foreground(p.Secondary),
		Selected: lipgloss.NewStyle().
			Bold(true).
			Foreground(p.Accent),
		Disabled: lipgloss.NewStyle().
			Foreground(p.Muted).
			Faint(true),
		Hint: lipgloss.NewStyle().
			Foreground(p.Muted).
			Faint(true),
		Label: lipgloss.NewStyle().
			Foreground(p.Secondary).
			Width(8),
		StatusOk: lipgloss.NewStyle().
			Foreground(p.Success).
			Bold(true),
		StatusWarn: lipgloss.NewStyle().
			Foreground(p.Warning),
		MenuItem: lipgloss.NewStyle().
			PaddingLeft(2),
		Card: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Border).
			Padding(0, 1).
			Margin(1, 0),
		// Output styles
		OutputLabel: lipgloss.NewStyle().
			Foreground(p.Secondary).
			Width(10).
			Bold(true),
		OutputValue: lipgloss.NewStyle().
			Foreground(p.Primary),
		OutputTitle: lipgloss.NewStyle().
			Foreground(p.Primary).
			Bold(true).
			MarginBottom(1),
	}
}

// hasDarkBackground detects terminal background color.
func hasDarkBackground() bool {
	// Simple heuristic: use COLORFGBG env var if available
	// COLORFGBG format: "0;15" or "15;0" (fg;bg)
	// Low bg number (0-7) usually means dark background
	if fgBg := os.Getenv("COLORFGBG"); fgBg != "" {
		parts := strings.Split(fgBg, ";")
		if len(parts) >= 2 {
			bgNum := parts[len(parts)-1]
			// ANSI colors 0-7 are typically dark, 8-15 are typically light
			for _, dark := range []string{"0", "1", "2", "3", "4", "5", "6", "7"} {
				if bgNum == dark {
					return true
				}
			}
			return false
		}
	}
	// Default to dark theme when we can't determine
	return true
}

// AdaptiveTheme returns a theme adapted to the terminal background.
func AdaptiveTheme() Theme {
	if hasDarkBackground() {
		return DefaultTheme()
	}
	return LightTheme()
}

// theme is the global theme instance, initialized at startup.
var theme = DefaultTheme()