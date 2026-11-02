// Package ui holds Skiff's styled terminal output.
package ui

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	copper = lipgloss.AdaptiveColor{Light: "#A4531F", Dark: "#E39155"}

	brand = lipgloss.NewStyle().Bold(true).Foreground(copper)
	green = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#0F6B4A", Dark: "#8FDCB9"})
	red   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#B3261E", Dark: "#F4978E"})
	muted = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#4B655F", Dark: "#A6BCB6"})
	label = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#4B655F", Dark: "#A6BCB6"}).Width(9).Inline(true)
	link  = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(copper)
)

func Banner(version string) {
	fmt.Println()
	fmt.Println("  " + brand.Render("Skiff") + " " + muted.Render("v"+version))
	fmt.Println()
}

func Accent(s string) string { return brand.Render(s) }

func Field(name, value string) {
	fmt.Println("  " + label.Render(name) + " " + value)
}

func Step(msg string) {
	fmt.Println("  " + muted.Render("→ "+msg))
}

func Done(msg string) {
	fmt.Println("  " + green.Render("✓") + " " + msg)
}

// Fail writes to stderr (not stdout) so it never corrupts the data stream of a piped command.
func Fail(msg string) {
	fmt.Fprintln(os.Stderr, "  "+red.Render("✗")+" "+msg)
}

func Note(s string) {
	fmt.Println("  " + muted.Render(s))
}

func Live(url string, d time.Duration) {
	fmt.Println("  " + green.Render("✓ Live at") + " " + link.Render(url) +
		"  " + muted.Render(fmt.Sprintf("(%.1fs)", d.Seconds())))
}
