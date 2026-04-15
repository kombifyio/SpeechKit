package assets

import _ "embed"

//go:embed Bubble_Icon.png
var bubbleIcon []byte

//go:embed SpeechKit_Icon.png
var speechKitIcon []byte

//go:embed speechkit.ico
var speechKitICO []byte

func BubbleIcon() []byte {
	return bubbleIcon
}

func SpeechKitIcon() []byte {
	return speechKitIcon
}

// SpeechKitICO returns the .ico format icon for Windows taskbar display.
func SpeechKitICO() []byte {
	return speechKitICO
}
