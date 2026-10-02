package pairing

import (
	"errors"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

var errEmptyQR = errors.New("pairing: qr content required")

// QRSVG encodes content as a monochrome SVG QR. The pairing JSON is the
// usual input; DNS-SD TXT must never carry this.
func QRSVG(content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", errEmptyQR
	}
	code, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bits := code.Bitmap()
	if len(bits) == 0 {
		return "", errEmptyQR
	}
	n := len(bits)
	var b strings.Builder
	b.Grow(n * n)
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `)
	b.WriteString(strconv.Itoa(n))
	b.WriteByte(' ')
	b.WriteString(strconv.Itoa(n))
	b.WriteString(`" shape-rendering="crispEdges"><rect width="100%" height="100%" fill="#ffffff"/><path fill="#000000" d="`)
	for y, row := range bits {
		ys := strconv.Itoa(y)
		for x, on := range row {
			if !on {
				continue
			}
			b.WriteByte('M')
			b.WriteString(strconv.Itoa(x))
			b.WriteByte(' ')
			b.WriteString(ys)
			b.WriteString("h1v1h-1z")
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}
