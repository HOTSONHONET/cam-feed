package detector

import (
	"math"
	"sort"
)

func postprocessYOLOv8(output []float32, meta LetterboxMeta, cfg Config, labels []string) []Detection {
	// Expected: output shape [1, 84, 8400] flattened

	const numAnchors = 8400
	const numClasses = 80
	const channels = 4 + numClasses // 4 for box + 80 for class scores

	if len(output) != channels*numAnchors {
		return nil
	}

	type candidate struct {
		cls   int
		score float32
		box   [4]float32 // x1, y1, x2, y2 in original image pixels
	}

	candidates := make([]candidate, 0, numAnchors)

	scale := meta.Scale
	padX := float64(meta.PadX)
	padY := float64(meta.PadY)

	for i := 0; i < numAnchors; i++ {
		cx := output[0*numAnchors+i]
		cy := output[1*numAnchors+i]
		w := output[2*numAnchors+i]
		h := output[3*numAnchors+i]

		// best class
		bestCls := -1
		best := float32(0)
		base := 4 * numAnchors
		for c := 0; c < numClasses; c++ {
			s := output[base+c*numAnchors+i]
			if s > best {
				best = s
				bestCls = c
			}
		}

		if bestCls < 0 || best < cfg.ScoreThreshold {
			continue
		}

		// Convert from cxcywh to x1y1x2y2 in letterboxed input space
		x1 := float64(cx - w/2)
		y1 := float64(cy - h/2)
		x2 := float64(cx + w/2)
		y2 := float64(cy + h/2)

		// Undo letterbox: remove padding then rescale
		x1 = (x1 - padX) / float64(scale)
		y1 = (y1 - padY) / float64(scale)
		x2 = (x2 - padX) / float64(scale)
		y2 = (y2 - padY) / float64(scale)

		x1, y1, x2, y2 = float64(x1), float64(y1), float64(x2), float64(y2)

		// Clamp to original image bounds
		x1 = math.Max(0, x1)
		y1 = math.Max(0, y1)
		x2 = math.Min(float64(meta.OrigW), x2)
		y2 = math.Min(float64(meta.OrigH), y2)

		// Ignore invalid boxes
		if x2 <= x1 || y2 <= y1 {
			continue
		}

		candidates = append(candidates, candidate{
			cls:   bestCls,
			score: best,
			box:   [4]float32{float32(x1), float32(y1), float32(x2), float32(y2)},
		})
	}

	if len(candidates) == 0 {
		return nil
	}

	// Sort by score desc
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// NMS
	kept := make([]candidate, 0, len(candidates))

	for _, c := range candidates {
		suppress := false
		for _, k := range kept {
			if k.cls != c.cls {
				continue
			}

			if iou(k.box, c.box) > cfg.NmsIouThreshold {
				suppress = true
				break
			}
		}

		if !suppress {
			kept = append(kept, c)
			if len(kept) >= cfg.MaxDetections {
				break
			}
		}
	}

	// Convert to output type

	out := make([]Detection, 0, len(kept))
	for _, k := range kept {
		label := "unknown"
		if k.cls >= 0 && k.cls < len(labels) {
			label = labels[k.cls]
		}
		out = append(out, Detection{
			Label: label,
			Score: k.score,
			Box:   k.box,
		})
	}
	return out
}

func iou(a, b [4]float32) float32 {
	ax1, ay1, ax2, ay2 := a[0], a[1], a[2], a[3]
	bx1, by1, bx2, by2 := b[0], b[1], b[2], b[3]

	interX1 := math.Max(float64(ax1), float64(bx1))
	interY1 := math.Max(float64(ay1), float64(by1))
	interX2 := math.Min(float64(ax2), float64(bx2))
	interY2 := math.Min(float64(ay2), float64(by2))

	iw := interX2 - interX1
	ih := interY2 - interY1

	if iw <= 0 || ih <= 0 {
		return 0
	}

	interArea := math.Max(0, interX2-interX1) * math.Max(0, interY2-interY1)
	areaA := (ax2 - ax1) * (ay2 - ay1)
	areaB := (bx2 - bx1) * (by2 - by1)

	unionArea := float64(areaA) + float64(areaB) - interArea
	if unionArea <= 0 {
		return 0
	}

	return float32(interArea / unionArea)
}
