package conv

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"

	_ "image/gif"  // register GIF decoder
	_ "image/jpeg" // register JPEG decoder
	_ "image/png"  // register PNG decoder
)

// ConvertRaster embeds a raster image (PNG/JPG/…) as a single fill-engrave
// object, scaled to fit the material (minus margins) and anchored top-left.
// One canvas, one image; no vector nesting.
func ConvertRaster(data []byte, format string, opt Options) ([]byte, Summary, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, Summary{}, fmt.Errorf("decode %s image: %w", format, err)
	}
	W, H := float64(cfg.Width), float64(cfg.Height)
	if W <= 0 || H <= 0 {
		return nil, Summary{}, fmt.Errorf("image has zero dimensions")
	}

	margin := opt.Margin
	if margin <= 0 {
		margin = opt.Spacing
	}
	usableW := opt.MaterialW - 2*margin
	usableH := opt.MaterialH - 2*margin
	if usableW <= 0 || usableH <= 0 {
		return nil, Summary{}, fmt.Errorf("material %.1fx%.1f mm too small for a %.1f mm margin", opt.MaterialW, opt.MaterialH, margin)
	}
	scale := math.Min(usableW/W, usableH/H) // mm per pixel, preserve aspect

	mime := "image/" + format
	switch format {
	case "jpg", "jpeg":
		mime = "image/jpeg"
	}
	src := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)

	objID := "el-" + newUUID()
	canvasID := "canvas-" + newUUID()

	img := wwsImageObject{
		Type: "image", Version: "5.3.0",
		OriginX: "left", OriginY: "top",
		Left: round3(margin), Top: round3(margin),
		Width: W, Height: H,
		Fill: "#000000", Stroke: "#000000", StrokeWidth: 0,
		StrokeLineCap: "butt", StrokeLineJoin: "miter", StrokeMiterLimit: 4,
		ScaleX: scale, ScaleY: scale,
		Opacity: 1, Visible: true,
		FillRule: "nonzero", PaintFirst: "fill", GlobalCompositeOperation: "source-over",
		ID: objID, Name: "element", HasControls: true, IsLock: false,
		OriginStroke: "#000000", OriginStrokeForCut: "",
		Sequence: 1, ProcessMode: "fillEngrave",
		Src:     src,
		Filters: []any{map[string]any{"type": "Grayscale", "mode": "average", "FilterName": "Grayscale"}},
	}

	f := &wwsFile{
		Name:      opt.Name,
		Version:   "3.0.4",
		ProjectID: "project-" + newUUID(),
		Time:      opt.Time,
		ProcessList: map[string]wwsProc{
			objID: {
				Cut:         wwsPS{Power: 0, Speed: 250, Repeat: 1},
				Engrave:     wwsPS{Power: 0, Speed: 250, Repeat: 1},
				FillEngrave: wwsPS{Power: 0, Speed: 250, Repeat: 1},
				ProcessMode: "fillEngrave", OneWayScan: true,
				LineDesity: 200, Dpi: 254, DotDuration: 230, ImgRevertMode: "jarvis",
				BreakPointObj: wwsBreakPoint{BreakPointNum: 2, BreakPointSize: 0.4, IsAutoBreakPoint: true},
			},
		},
		CurrentCanvasID: canvasID,
		CanvasList: []wwsCanvas{{
			ID: canvasID, Name: "canvas01", Color: "#ffff00",
			Objects: []any{img},
			WorkModeData: wwsWorkModeData{
				CanvasID: canvasID, Perimeter: 2 * (opt.MaterialW + opt.MaterialH),
				Diameter: opt.MaterialW, WorkMode: "plane", PathPlanning: "AUTO_MERGE",
				ScanMode: "SCAN_ONE_WAY",
			},
		}},
	}
	empty := ""
	f.LayerDataList = []wwsLayerData{{
		ID: canvasID,
		Data: []wwsLayerEntry{
			{Type: "color", ID: "#000000", Color: "#000000", Show: true},
			{ID: objID, ParentGroupID: &empty, Base64: &empty, Type: "shape",
				Name: strPtr("editor.color_layer.image"), Color: "#000000", Show: true},
		},
	}}
	f.Cover = rasterCover(data)

	out, err := f.Marshal()
	if err != nil {
		return nil, Summary{}, err
	}
	return out, Summary{Pieces: 1, Sheets: 1, EngraveSheets: 1, Bytes: len(out)}, nil
}

// rasterCover builds a small PNG data-URI thumbnail (max 360px) for the library
// cover, falling back to the original data URI if decoding fails.
func rasterCover(data []byte) string {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	b := src.Bounds()
	const maxDim = 360
	scale := math.Min(float64(maxDim)/float64(b.Dx()), float64(maxDim)/float64(b.Dy()))
	if scale > 1 {
		scale = 1
	}
	tw, th := int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	thumb := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		sy := b.Min.Y + int(float64(y)/scale)
		for x := 0; x < tw; x++ {
			sx := b.Min.X + int(float64(x)/scale)
			thumb.Set(x, y, src.At(sx, sy))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, thumb); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}
