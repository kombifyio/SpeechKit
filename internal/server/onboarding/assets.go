package onboarding

import _ "embed"

//go:embed assets/testui.html
var testUIHTML string

//go:embed assets/setup.html
var setupUIHTML string

func TestUIHTML() string {
	return testUIHTML
}

func SetupUIHTML() string {
	return setupUIHTML
}
