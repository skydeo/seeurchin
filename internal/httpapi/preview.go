package httpapi

import (
	"bytes"
	_ "embed"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/enderu/seeurchin/internal/codes"
	"github.com/enderu/seeurchin/internal/poll"
)

// Link-preview (Open Graph) support. SvelteKit serves a single static
// index.html for every route, so crawlers (iMessage, WhatsApp, Slack, Discord,
// …) that don't run JavaScript see no per-poll metadata. These handlers
// intercept the poll route to (a) inject per-poll OG/Twitter tags into the
// served HTML and (b) generate a branded 1200x630 share card on the fly.

// Brand fonts (Baloo 2 — the --font-display family) embedded for server-side
// text rendering. Only Baloo 2 is vendored in-repo; the UI loads Quicksand /
// Nunito from a CDN at runtime, which isn't available here.
//
//go:embed assets/Baloo2-ExtraBold.ttf
var balooExtraBoldTTF []byte

//go:embed assets/Baloo2-SemiBold.ttf
var balooSemiBoldTTF []byte

//go:embed assets/Baloo2-Medium.ttf
var balooMediumTTF []byte

// Card dimensions — the standard "summary_large_image" size every platform
// crops to.
const (
	cardW = 1200
	cardH = 630
)

// wmAlpha is the opacity (0–255) the full-color watermark layer is composited
// at — low enough that the brand colors stay subdued background texture.
const wmAlpha = 30

// Brand palette (dark "ocean" theme). See docs/design-system.md.
var (
	colBG       = mustHex("#072e3d") // deep ocean background
	colText     = mustHex("#eaf4f1") // primary light text
	colMuted    = mustHex("#7fa2a8") // footer / secondary
	colPill     = mustHex("#0d4a5f") // code pill surface
	colPillLine = mustHex("#1d7aa3") // code pill border (ocean primary)
	colCode     = mustHex("#ffd76e") // code text (sun)

	// Urchin mark: 12 spines cycling through 4 colors, plus a two-tone core.
	colCoral = mustHex("#ff6f5e")
	colTeal  = mustHex("#11b3aa")
	colMango = mustHex("#ffa23a")
	colOcean = mustHex("#2b88ad") // lightened from #0e5a7d so spines read on dark
	colCore  = mustHex("#0b3a49") // inner core ring
	colSun   = mustHex("#ffce5c") // core pupil
)

// fontsOnce guards one-time parsing of the embedded TTFs.
var (
	fontsOnce                            sync.Once
	fontsErr                             error
	fontExtraBold, fontSemiBold, fontMed *opentype.Font
	previewCache                         sync.Map // key "code|title" -> []byte
)

func loadFonts() error {
	fontsOnce.Do(func() {
		if fontExtraBold, fontsErr = opentype.Parse(balooExtraBoldTTF); fontsErr != nil {
			return
		}
		if fontSemiBold, fontsErr = opentype.Parse(balooSemiBoldTTF); fontsErr != nil {
			return
		}
		fontMed, fontsErr = opentype.Parse(balooMediumTTF)
	})
	return fontsErr
}

func face(f *opentype.Font, px float64) font.Face {
	fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingFull})
	if err != nil { // sizes are constants; this never fails in practice
		panic(err)
	}
	return fc
}

// --- HTTP handlers ---

// handlePollPage serves index.html with per-poll Open Graph tags injected into
// <head>. Real browsers boot the SPA exactly as before (the tags are inert for
// them); crawlers get a rich card. Unknown codes fall back to the plain SPA.
func (s *Server) handlePollPage(w http.ResponseWriter, r *http.Request) {
	body, err := s.indexHTML()
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	code := codes.Normalize(chi.URLParam(r, "code"))
	if p, err := s.repo.GetPollByCode(r.Context(), code); err == nil && p != nil {
		body = injectHead(body, s.ogTags(p))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}

// handlePollPreview generates (and caches) the share card PNG for a poll.
func (s *Server) handlePollPreview(w http.ResponseWriter, r *http.Request) {
	code := codes.Normalize(chi.URLParam(r, "code"))
	p, err := s.repo.GetPollByCode(r.Context(), code)
	if err != nil || p == nil {
		http.NotFound(w, r)
		return
	}
	cacheKey := p.Code + "|" + p.Title
	var pngBytes []byte
	if v, ok := previewCache.Load(cacheKey); ok {
		pngBytes = v.([]byte)
	} else {
		pngBytes, err = renderCard(p.Title, p.Code, hostFromBase(s.cfg.BaseURL))
		if err != nil {
			http.Error(w, "preview unavailable", http.StatusInternalServerError)
			return
		}
		previewCache.Store(cacheKey, pngBytes)
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(pngBytes)
}

// --- HTML head injection ---

var (
	indexOnce  sync.Once
	indexBytes []byte
	indexErr   error
)

func (s *Server) indexHTML() ([]byte, error) {
	indexOnce.Do(func() {
		indexBytes, indexErr = webDist.ReadFile("webdist/index.html")
	})
	return indexBytes, indexErr
}

// injectHead inserts tags immediately before the first </head>.
func injectHead(htmlDoc []byte, tags string) []byte {
	const marker = "</head>"
	i := bytes.Index(htmlDoc, []byte(marker))
	if i < 0 {
		return htmlDoc
	}
	out := make([]byte, 0, len(htmlDoc)+len(tags))
	out = append(out, htmlDoc[:i]...)
	out = append(out, tags...)
	out = append(out, htmlDoc[i:]...)
	return out
}

func (s *Server) ogTags(p *poll.Poll) string {
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = "Movie night vote"
	}
	base := strings.TrimRight(s.cfg.BaseURL, "/")
	pageURL := base + "/p/" + p.Code
	imgURL := pageURL + "/preview.png"
	desc := fmt.Sprintf("Join the vote · code %s", p.Code)

	e := html.EscapeString
	var b strings.Builder
	b.WriteString("\n\t\t<!-- Link-preview metadata (injected per poll) -->\n")
	add := func(format string, args ...any) {
		b.WriteString("\t\t")
		fmt.Fprintf(&b, format, args...)
		b.WriteByte('\n')
	}
	add(`<meta property="og:type" content="website" />`)
	add(`<meta property="og:site_name" content="seeurchin" />`)
	add(`<meta property="og:title" content=%q />`, e(title))
	add(`<meta property="og:description" content=%q />`, e(desc))
	add(`<meta property="og:url" content=%q />`, e(pageURL))
	add(`<meta property="og:image" content=%q />`, e(imgURL))
	add(`<meta property="og:image:type" content="image/png" />`)
	add(`<meta property="og:image:width" content="1200" />`)
	add(`<meta property="og:image:height" content="630" />`)
	add(`<meta property="og:image:alt" content=%q />`, e(title+" — seeurchin"))
	add(`<meta name="twitter:card" content="summary_large_image" />`)
	add(`<meta name="twitter:title" content=%q />`, e(title))
	add(`<meta name="twitter:description" content=%q />`, e(desc))
	add(`<meta name="twitter:image" content=%q />`, e(imgURL))
	add(`<meta name="description" content=%q />`, e(desc))
	return b.String()
}

func hostFromBase(base string) string {
	h := base
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	return strings.TrimRight(h, "/")
}

// --- card rendering ---

func renderCard(title, code, host string) ([]byte, error) {
	if err := loadFonts(); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, cardW, cardH))
	draw.Draw(img, img.Bounds(), image.NewUniform(colBG), image.Point{}, draw.Src)

	// Faint oversized mark bleeding off the top-right as background texture.
	// Rendered full-color on its own layer, then the whole layer is composited
	// at very low alpha — so the brand colors show through, subdued, without
	// overlapping spines darkening at the hub.
	wm := image.NewRGBA(img.Bounds())
	drawMark(wm, 720, -150, 6.8)
	draw.DrawMask(img, img.Bounds(), wm, image.Point{}, image.NewUniform(color.Alpha{A: wmAlpha}), image.Point{}, draw.Over)

	const margin = 84

	// --- Lockup: urchin mark + "seeurchin" wordmark ---
	const markSize = 100
	markY := float64(70)
	drawMark(img, margin, markY, markSize/100.0)

	wordFace := face(fontExtraBold, 62)
	wordX := margin + markSize + 26
	// Vertically center the wordmark on the mark.
	wordBaseline := int(markY) + markSize/2 + capCenterOffset(wordFace)
	drawText(img, wordFace, colText, "seeurchin", wordX, wordBaseline, 0)

	// --- Poll title ---
	if t := strings.TrimSpace(title); t != "" {
		titleFace := face(fontSemiBold, 56)
		t = fitText(titleFace, t, cardW-2*margin, 0)
		drawText(img, titleFace, colText, t, margin, 312, 0)
	}

	// --- Code pill ---
	codeFace := face(fontExtraBold, 110)
	tracking := fixed.I(10)
	codeWpx := measure(codeFace, code, tracking).Ceil()
	const padX, pillH, pillY = 56, 158, 356
	pillW := codeWpx + 2*padX
	drawRoundRect(img, margin, pillY, pillW, pillH, 34, colPill, colPillLine, 3)
	codeBaseline := pillY + pillH/2 + capCenterOffset(codeFace)
	drawText(img, codeFace, colCode, code, margin+padX, codeBaseline, tracking)

	// --- Footer ---
	footFace := face(fontMed, 30)
	footBaseline := cardH - 46
	drawText(img, footFace, colMuted, host, margin, footBaseline, 0)
	tag := "movie night vote"
	tagW := measure(footFace, tag, 0).Ceil()
	drawText(img, footFace, colMuted, tag, cardW-margin-tagW, footBaseline, 0)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// urchinSpines is the seeurchin mark's 12 spine segments on its native 100x100
// artboard, each with the brand color it's stroked in. (x1,y1) is the inner
// base (tucked under the hub at r=13.5 so the arm reads as attached); (x2,y2)
// is the outer tip. Outer tips are deliberately irregular for an organic
// silhouette.
var urchinSpines = []struct {
	x1, y1, x2, y2 float64
	c              color.RGBA
}{
	{50, 39, 50, 4, colCoral},
	{55.5, 40.47, 69.5, 16.23, colTeal},
	{59.53, 44.5, 87.24, 28.5, colMango},
	{61, 50, 96, 50, colOcean},
	{59.53, 55.5, 83.77, 69.5, colCoral},
	{55.5, 59.53, 71.5, 87.24, colTeal},
	{50, 61, 50, 96, colMango},
	{44.5, 59.53, 30.5, 83.77, colOcean},
	{40.47, 55.5, 12.76, 71.5, colCoral},
	{39, 50, 4, 50, colTeal},
	{40.47, 44.5, 16.23, 30.5, colMango},
	{44.5, 40.47, 28.5, 12.76, colOcean},
}

// drawMark renders the full-color urchin (12 spines + two-tone core) at the
// given top-left origin, scaled from its native 100x100 artboard. Spines have a
// flat inner base and a rounded outer tip; the core is a clear two-tone hub.
func drawMark(dst draw.Image, ox, oy, scale float64) {
	tx := func(v float64) float32 { return float32(ox + v*scale) }
	ty := func(v float64) float32 { return float32(oy + v*scale) }
	hw := float32(3 * scale) // half of the stroke-width 6
	for _, sp := range urchinSpines {
		// (x1,y1) inner = flat base, (x2,y2) outer = round tip.
		fillSpine(dst, tx(sp.x1), ty(sp.y1), tx(sp.x2), ty(sp.y2), hw, sp.c)
	}
	// Core: clear ink ring (r=13.5) then sun inner circle (r=6.5). Drawn after
	// the spines so it cleanly covers their flat bases.
	fillCircle(dst, tx(50), ty(50), float32(13.5*scale), colCore)
	fillCircle(dst, tx(50), ty(50), float32(6.5*scale), colSun)
}

// fillSpine rasterizes a shaft with a FLAT inner cap at (x1,y1) and a ROUND
// outer cap at (x2,y2) — the urchin-arm shape. (Compare fillCapsule, which
// rounds both ends.)
func fillSpine(dst draw.Image, x1, y1, x2, y2, hw float32, c color.Color) {
	z := vector.NewRasterizer(dst.Bounds().Dx(), dst.Bounds().Dy())
	ang := math.Atan2(float64(y2-y1), float64(x2-x1))
	na := ang + math.Pi/2
	ax := float32(math.Cos(na)) * hw
	ay := float32(math.Sin(na)) * hw
	z.MoveTo(x1+ax, y1+ay)             // inner base, side A
	z.LineTo(x2+ax, y2+ay)             // outer, side A
	arc(z, x2, y2, hw, na, na-math.Pi) // round outer cap (sweep through the tip)
	z.LineTo(x1-ax, y1-ay)             // outer side B → inner base, side B
	z.ClosePath()                      // straight FLAT edge across the inner base
	z.Draw(dst, dst.Bounds(), image.NewUniform(c), image.Point{})
}

// fillCapsule rasterizes a round-capped line (a "stadium") in src color.
// Retained for fillCircle (zero-length capsule).
func fillCapsule(dst draw.Image, x1, y1, x2, y2, hw float32, c color.Color) {
	z := vector.NewRasterizer(dst.Bounds().Dx(), dst.Bounds().Dy())
	ang := math.Atan2(float64(y2-y1), float64(x2-x1))
	na := ang + math.Pi/2
	ax := float32(math.Cos(na)) * hw
	ay := float32(math.Sin(na)) * hw
	z.MoveTo(x1+ax, y1+ay)
	z.LineTo(x2+ax, y2+ay)
	arc(z, x2, y2, hw, na, na-math.Pi) // cap p2: sweep through the outward tip
	z.LineTo(x1-ax, y1-ay)
	arc(z, x1, y1, hw, na+math.Pi, na) // cap p1: sweep through the outward tip
	z.ClosePath()
	z.Draw(dst, dst.Bounds(), image.NewUniform(c), image.Point{})
}

func fillCircle(dst draw.Image, cx, cy, r float32, c color.Color) {
	fillCapsule(dst, cx, cy, cx, cy, r, c) // zero-length capsule == circle
}

// arc appends a polyline approximation of a circular arc to the rasterizer.
func arc(z *vector.Rasterizer, cx, cy, r float32, a0, a1 float64) {
	const seg = 16
	for i := 1; i <= seg; i++ {
		t := a0 + (a1-a0)*float64(i)/seg
		z.LineTo(cx+r*float32(math.Cos(t)), cy+r*float32(math.Sin(t)))
	}
}

// drawRoundRect fills a rounded rectangle with fill and strokes a border.
func drawRoundRect(dst draw.Image, x, y, w, h, rad int, fill, border color.Color, bw int) {
	roundRectPath := func(z *vector.Rasterizer, x, y, w, h, rad float32) {
		z.MoveTo(x+rad, y)
		z.LineTo(x+w-rad, y)
		arc(z, x+w-rad, y+rad, rad, -math.Pi/2, 0)
		z.LineTo(x+w, y+h-rad)
		arc(z, x+w-rad, y+h-rad, rad, 0, math.Pi/2)
		z.LineTo(x+rad, y+h)
		arc(z, x+rad, y+h-rad, rad, math.Pi/2, math.Pi)
		z.LineTo(x, y+rad)
		arc(z, x+rad, y+rad, rad, math.Pi, 3*math.Pi/2)
		z.ClosePath()
	}
	fx, fy, fw, fh, fr := float32(x), float32(y), float32(w), float32(h), float32(rad)
	if bw > 0 {
		z := vector.NewRasterizer(dst.Bounds().Dx(), dst.Bounds().Dy())
		roundRectPath(z, fx, fy, fw, fh, fr)
		z.Draw(dst, dst.Bounds(), image.NewUniform(border), image.Point{})
	}
	b := float32(bw)
	z := vector.NewRasterizer(dst.Bounds().Dx(), dst.Bounds().Dy())
	roundRectPath(z, fx+b, fy+b, fw-2*b, fh-2*b, fr-b)
	z.Draw(dst, dst.Bounds(), image.NewUniform(fill), image.Point{})
}

// --- text helpers ---

// drawText draws s with its baseline at y, left edge at x, optional per-rune
// tracking (extra advance).
func drawText(dst draw.Image, fc font.Face, c color.Color, s string, x, y int, tracking fixed.Int26_6) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c),
		Face: fc,
		Dot:  fixed.P(x, y),
	}
	if tracking == 0 {
		d.DrawString(s)
		return
	}
	for _, r := range s {
		d.DrawString(string(r))
		d.Dot.X += tracking
	}
}

func measure(fc font.Face, s string, tracking fixed.Int26_6) fixed.Int26_6 {
	w := font.MeasureString(fc, s)
	if tracking != 0 {
		if n := len([]rune(s)); n > 1 {
			w += tracking * fixed.Int26_6(n-1)
		}
	}
	return w
}

// capCenterOffset returns how far below the baseline-anchored Dot.Y the text's
// optical center sits, so callers can vertically center on a point: pass a
// center Y and add this offset to get the baseline Y.
func capCenterOffset(fc font.Face) int {
	m := fc.Metrics()
	return (m.Ascent - m.Descent).Round() / 2
}

// fitText trims s with an ellipsis until it fits within maxW pixels.
func fitText(fc font.Face, s string, maxW int, tracking fixed.Int26_6) string {
	if measure(fc, s, tracking).Ceil() <= maxW {
		return s
	}
	runes := []rune(s)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		cand := strings.TrimRight(string(runes), " ") + "…"
		if measure(fc, cand, tracking).Ceil() <= maxW {
			return cand
		}
	}
	return "…"
}

func mustHex(s string) color.RGBA {
	s = strings.TrimPrefix(s, "#")
	var r, g, b uint8
	fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	return color.RGBA{r, g, b, 255}
}
