package conv

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
)

const cutRed = "#E61F19"

// BuildOptions carries the non-geometry settings needed to emit a file.
type BuildOptions struct {
	Name      string
	CutPower  int
	CutSpeed  int
	CutPasses int
	MaterialW float64
	MaterialH float64
	Time      int64
}

// ---- JSON model (mirrors the v3.0.4 field sets observed in real files) ----

type wwsFile struct {
	Name            string             `json:"name"`
	Version         string             `json:"version"`
	ProjectID       string             `json:"projectId"`
	CanvasList      []wwsCanvas        `json:"canvasList"`
	ProcessList     map[string]wwsProc `json:"processList"`
	Time            int64              `json:"time"`
	CurrentCanvasID string             `json:"currentCanvasId"`
	Cover           string             `json:"cover"`
	LayerDataList   []wwsLayerData     `json:"layerDataList"`
}

type wwsCanvas struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Color        string          `json:"color"`
	Objects      []wwsObject     `json:"objects"`
	WorkModeData wwsWorkModeData `json:"workModeData"`
}

type wwsWorkModeData struct {
	CanvasID          string  `json:"canvasID"`
	Perimeter         float64 `json:"perimeter"`
	Diameter          float64 `json:"diameter"`
	WorkMode          string  `json:"workMode"`
	PathPlanning      string  `json:"pathPlanning"`
	BackgroundImage   any     `json:"backgroundImage"`
	BaseplateDistance float64 `json:"baseplateDistance"`
	ScanMode          string  `json:"scanMode"`
}

type wwsObject struct {
	Type                     string  `json:"type"`
	Version                  string  `json:"version"`
	OriginX                  string  `json:"originX"`
	OriginY                  string  `json:"originY"`
	Left                     float64 `json:"left"`
	Top                      float64 `json:"top"`
	Width                    float64 `json:"width"`
	Height                   float64 `json:"height"`
	Fill                     string  `json:"fill"`
	Stroke                   string  `json:"stroke"`
	StrokeWidth              float64 `json:"strokeWidth"`
	StrokeDashArray          any     `json:"strokeDashArray"`
	StrokeLineCap            string  `json:"strokeLineCap"`
	StrokeDashOffset         float64 `json:"strokeDashOffset"`
	StrokeLineJoin           string  `json:"strokeLineJoin"`
	StrokeUniform            bool    `json:"strokeUniform"`
	StrokeMiterLimit         float64 `json:"strokeMiterLimit"`
	ScaleX                   float64 `json:"scaleX"`
	ScaleY                   float64 `json:"scaleY"`
	Angle                    float64 `json:"angle"`
	FlipX                    bool    `json:"flipX"`
	FlipY                    bool    `json:"flipY"`
	Opacity                  float64 `json:"opacity"`
	Shadow                   any     `json:"shadow"`
	Visible                  bool    `json:"visible"`
	BackgroundColor          string  `json:"backgroundColor"`
	FillRule                 string  `json:"fillRule"`
	PaintFirst               string  `json:"paintFirst"`
	GlobalCompositeOperation string  `json:"globalCompositeOperation"`
	SkewX                    float64 `json:"skewX"`
	SkewY                    float64 `json:"skewY"`
	ID                       string  `json:"id"`
	Name                     string  `json:"name"`
	HasControls              bool    `json:"hasControls"`
	IsLock                   bool    `json:"isLock"`
	OriginStroke             string  `json:"originStroke"`
	OriginStrokeForCut       string  `json:"originStrokeForCut"`
	Sequence                 int     `json:"sequence"`
	ProcessMode              string  `json:"processMode"`
	Path                     [][]any `json:"path"`
}

type wwsProc struct {
	Cut             wwsPS         `json:"cut"`
	Engrave         wwsPS         `json:"engrave"`
	FillEngrave     wwsPS         `json:"fillEngrave"`
	ProcessMode     string        `json:"processMode"`
	IsLock          bool          `json:"isLock"`
	StrengthCutting bool          `json:"strengthCutting"`
	OneWayScan      bool          `json:"oneWayScan"`
	LineDesity      int           `json:"lineDesity"`
	Dpi             int           `json:"dpi"`
	DotDuration     int           `json:"dotDuration"`
	BreakPointObj   wwsBreakPoint `json:"breakPointObj"`
	ProcessAngle    float64       `json:"processAngle"`
	Fixed           bool          `json:"fixed"`
	Radius          float64       `json:"radius"`
}

type wwsPS struct {
	Power  int `json:"power"`
	Speed  int `json:"speed"`
	Repeat int `json:"repeat"`
}

type wwsBreakPoint struct {
	BreakPointNum    int     `json:"breakPointNum"`
	BreakPointSize   float64 `json:"breakPointSize"`
	IsBreakPoint     bool    `json:"isBreakPoint"`
	IsAutoBreakPoint bool    `json:"isAutoBreakPoint"`
}

type wwsLayerData struct {
	ID   string          `json:"id"`
	Data []wwsLayerEntry `json:"data"`
}

type wwsLayerEntry struct {
	Type          string  `json:"type"`
	ID            string  `json:"id"`
	ParentGroupID *string `json:"parentGroupID,omitempty"`
	Base64        *string `json:"base64,omitempty"`
	Name          *string `json:"name,omitempty"`
	Color         string  `json:"color"`
	Selected      bool    `json:"selected"`
	Show          bool    `json:"show"`
	Fixed         bool    `json:"fixed"`
}

// Build assembles a complete .wws document from the placements.
func Build(placements []Placement, nSheets int, opt BuildOptions) (*wwsFile, error) {
	f := &wwsFile{
		Name:        opt.Name,
		Version:     "3.0.4",
		ProjectID:   "project-" + newUUID(),
		ProcessList: map[string]wwsProc{},
		Time:        opt.Time,
	}

	// One canvas per sheet.
	canvasIDs := make([]string, nSheets)
	canvases := make([]wwsCanvas, nSheets)
	layers := make([]wwsLayerData, nSheets)
	for i := 0; i < nSheets; i++ {
		id := "canvas-" + newUUID()
		canvasIDs[i] = id
		canvases[i] = wwsCanvas{
			ID:    id,
			Name:  fmt.Sprintf("canvas%02d", i+1),
			Color: "#ffff00",
			WorkModeData: wwsWorkModeData{
				CanvasID:          id,
				Perimeter:         2 * (opt.MaterialW + opt.MaterialH),
				Diameter:          opt.MaterialW,
				WorkMode:          "plane",
				PathPlanning:      "AUTO_MERGE",
				BaseplateDistance: 0,
				ScanMode:          "SCAN_ONE_WAY",
			},
		}
		layers[i] = wwsLayerData{
			ID: id,
			Data: []wwsLayerEntry{{
				Type: "color", ID: cutRed, Color: cutRed, Show: true,
			}},
		}
	}

	seq := 0
	for _, pl := range placements {
		if pl.Sheet < 0 || pl.Sheet >= nSheets {
			return nil, fmt.Errorf("placement references sheet %d of %d", pl.Sheet, nSheets)
		}
		seq++
		objID := "el-" + newUUID()

		// Transform the exact geometry into absolute sheet-mm path commands.
		transformed := make([]Subpath, len(pl.Piece.Subpaths))
		for i, sp := range pl.Piece.Subpaths {
			transformed[i] = sp.transform(pl.M)
		}
		bb := subpathsBBox(transformed)
		pathCmds := toPathArray(transformed)

		obj := wwsObject{
			Type: "path", Version: "5.3.0",
			OriginX: "left", OriginY: "top",
			Left: round3(bb.MinX), Top: round3(bb.MinY),
			Width: round3(bb.W()), Height: round3(bb.H()),
			Fill: "", Stroke: cutRed, StrokeWidth: 0,
			StrokeLineCap: "round", StrokeDashOffset: 0, StrokeLineJoin: "round",
			StrokeUniform: true, StrokeMiterLimit: 4,
			ScaleX: 1, ScaleY: 1, Angle: 0,
			Opacity: 1, Visible: true,
			FillRule: "nonzero", PaintFirst: "fill", GlobalCompositeOperation: "source-over",
			ID: objID, Name: "element", HasControls: true, IsLock: false,
			OriginStroke: "#FFA500", OriginStrokeForCut: cutRed,
			Sequence: seq, ProcessMode: "cut",
			Path: pathCmds,
		}
		canvases[pl.Sheet].Objects = append(canvases[pl.Sheet].Objects, obj)

		f.ProcessList[objID] = wwsProc{
			Cut:         wwsPS{Power: opt.CutPower, Speed: opt.CutSpeed, Repeat: cutRepeat(opt.CutPasses)},
			Engrave:     wwsPS{Power: 0, Speed: 2, Repeat: 1},
			FillEngrave: wwsPS{Power: 0, Speed: 2, Repeat: 1},
			ProcessMode: "cut", OneWayScan: true, LineDesity: 100,
			Dpi: 254, DotDuration: 230,
			BreakPointObj: wwsBreakPoint{BreakPointNum: 2, BreakPointSize: 0.4, IsAutoBreakPoint: true},
		}

		empty := ""
		layers[pl.Sheet].Data = append(layers[pl.Sheet].Data, wwsLayerEntry{
			ID: objID, ParentGroupID: &empty, Base64: &empty,
			Type: "shape", Name: strPtr("editor.color_layer.path"),
			Color: cutRed, Show: true,
		})
	}

	f.CanvasList = canvases
	f.LayerDataList = layers
	if nSheets > 0 {
		f.CurrentCanvasID = canvasIDs[0]
	}
	return f, nil
}

// Marshal serialises the file to compact JSON.
func (f *wwsFile) Marshal() ([]byte, error) { return json.Marshal(f) }

// toPathArray converts subpaths into Fabric path command arrays.
func toPathArray(sps []Subpath) [][]any {
	var out [][]any
	for _, sp := range sps {
		for _, c := range sp.Cmds {
			switch c.Op {
			case OpM:
				out = append(out, []any{"M", round3(c.P[0].X), round3(c.P[0].Y)})
			case OpL:
				out = append(out, []any{"L", round3(c.P[0].X), round3(c.P[0].Y)})
			case OpQ:
				out = append(out, []any{"Q", round3(c.P[0].X), round3(c.P[0].Y), round3(c.P[1].X), round3(c.P[1].Y)})
			case OpC:
				out = append(out, []any{"C",
					round3(c.P[0].X), round3(c.P[0].Y),
					round3(c.P[1].X), round3(c.P[1].Y),
					round3(c.P[2].X), round3(c.P[2].Y)})
			case OpZ:
				out = append(out, []any{"Z"})
			}
		}
	}
	return out
}

func round3(v float64) float64 {
	r := math.Round(v*1000) / 1000
	if r == 0 {
		return 0 // avoid -0
	}
	return r
}

func cutRepeat(passes int) int {
	if passes < 1 {
		return 1
	}
	return passes
}

func strPtr(s string) *string { return &s }

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
