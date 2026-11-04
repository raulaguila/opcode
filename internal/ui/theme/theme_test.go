package theme

import (
	"slices"
	"testing"
)

func TestThemeRegistration(t *testing.T) {
	// Get list of available themes
	availableThemes := AvailableThemes()

	// Check if "catppuccin" theme is registered
	catppuccinFound := false
	testThemeRegistrationAvailableThemes(availableThemes, &catppuccinFound)

	if !catppuccinFound {
		t.Errorf("Catppuccin theme is not registered")
	}

	// Check if "gruvbox" theme is registered
	gruvboxFound := false
	testThemeRegistrationAvailableThemesCase(availableThemes, &gruvboxFound)

	if !gruvboxFound {
		t.Errorf("Gruvbox theme is not registered")
	}

	// Check if "monokai" theme is registered
	monokaiFound := slices.Contains(availableThemes, "monokai")

	if !monokaiFound {
		t.Errorf("Monokai theme is not registered")
	}

	// Try to get the themes and make sure they're not nil
	catppuccin := GetTheme("catppuccin")
	if catppuccin == nil {
		t.Errorf("Catppuccin theme is nil")
	}

	gruvbox := GetTheme("gruvbox")
	if gruvbox == nil {
		t.Errorf("Gruvbox theme is nil")
	}

	monokai := GetTheme("monokai")
	if monokai == nil {
		t.Errorf("Monokai theme is nil")
	}

	// Test switching theme
	originalTheme := CurrentThemeName()

	err := SetTheme("gruvbox")
	if err != nil {
		t.Errorf("Failed to set theme to gruvbox: %v", err)
	}

	if CurrentThemeName() != "gruvbox" {
		t.Errorf("Theme not properly switched to gruvbox")
	}

	err = SetTheme("monokai")
	if err != nil {
		t.Errorf("Failed to set theme to monokai: %v", err)
	}

	if CurrentThemeName() != "monokai" {
		t.Errorf("Theme not properly switched to monokai")
	}

	// Switch back to original theme
	_ = SetTheme(originalTheme)
}

func testThemeRegistrationAvailableThemes(availableThemes []string, catppuccinFound *bool) {
	if slices.Contains(availableThemes, "catppuccin") {
		(*catppuccinFound) = true
	}
}

func testThemeRegistrationAvailableThemesCase(availableThemes []string, gruvboxFound *bool) {
	if slices.Contains(availableThemes, "gruvbox") {
		(*gruvboxFound) = true
	}
}
