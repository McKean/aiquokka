package cmd

import (
	"fmt"
	"math"
	"strconv"
)

type point struct{ x, y float64 }

type pathScanner struct {
	s   string
	pos int
}

func (p *pathScanner) skipSeparators() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', ',', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *pathScanner) done() bool {
	p.skipSeparators()
	return p.pos >= len(p.s)
}

func (p *pathScanner) peekCommand() (byte, bool) {
	p.skipSeparators()
	if p.pos >= len(p.s) {
		return 0, false
	}
	c := p.s[p.pos]
	if (c >= 'a' && c <= 'z' && c != 'e') || (c >= 'A' && c <= 'Z' && c != 'E') {
		return c, true
	}
	return 0, false
}

func (p *pathScanner) number() (float64, error) {
	p.skipSeparators()
	start := p.pos
	if p.pos < len(p.s) && (p.s[p.pos] == '-' || p.s[p.pos] == '+') {
		p.pos++
	}
	seenDot, seenDigit := false, false
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		switch {
		case c >= '0' && c <= '9':
			seenDigit = true
			p.pos++
		case c == '.' && !seenDot:
			seenDot = true
			p.pos++
		case (c == 'e' || c == 'E') && seenDigit:
			p.pos++
			if p.pos < len(p.s) && (p.s[p.pos] == '-' || p.s[p.pos] == '+') {
				p.pos++
			}
		default:
			goto end
		}
	}
end:
	if !seenDigit {
		return 0, fmt.Errorf("expected number at %d in path", start)
	}
	return strconv.ParseFloat(p.s[start:p.pos], 64)
}

func (p *pathScanner) flag() (bool, error) {
	p.skipSeparators()
	if p.pos >= len(p.s) || (p.s[p.pos] != '0' && p.s[p.pos] != '1') {
		return false, fmt.Errorf("expected arc flag at %d in path", p.pos)
	}
	v := p.s[p.pos] == '1'
	p.pos++
	return v, nil
}

func (p *pathScanner) numbers(n int) ([]float64, error) {
	out := make([]float64, n)
	for i := range out {
		v, err := p.number()
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

const curveSteps = 12

func parseSVGPath(d string) ([][]point, error) {
	sc := &pathScanner{s: d}
	var (
		subpaths    [][]point
		current     []point
		cur, start  point
		lastCtrl    point
		lastCmd     byte
		cmd         byte
		haveCommand bool
	)
	closeSub := func() {
		if len(current) > 1 {
			subpaths = append(subpaths, current)
		}
		current = nil
	}
	lineTo := func(q point) {
		if len(current) == 0 {
			current = append(current, cur)
		}
		current = append(current, q)
		cur = q
	}
	cubic := func(c1, c2, end point) {
		p0 := cur
		for i := 1; i <= curveSteps; i++ {
			t := float64(i) / curveSteps
			mt := 1 - t
			lineTo(point{
				mt*mt*mt*p0.x + 3*mt*mt*t*c1.x + 3*mt*t*t*c2.x + t*t*t*end.x,
				mt*mt*mt*p0.y + 3*mt*mt*t*c1.y + 3*mt*t*t*c2.y + t*t*t*end.y,
			})
		}
		lastCtrl = c2
	}
	quad := func(c, end point) {
		p0 := cur
		for i := 1; i <= curveSteps; i++ {
			t := float64(i) / curveSteps
			mt := 1 - t
			lineTo(point{mt*mt*p0.x + 2*mt*t*c.x + t*t*end.x, mt*mt*p0.y + 2*mt*t*c.y + t*t*end.y})
		}
		lastCtrl = c
	}

	for !sc.done() {
		if c, ok := sc.peekCommand(); ok {
			cmd = c
			sc.pos++
			haveCommand = true
		} else if !haveCommand {
			return nil, fmt.Errorf("path must start with a command")
		}
		rel := cmd >= 'a' && cmd <= 'z'
		off := point{}
		if rel {
			off = cur
		}
		upper := cmd &^ 0x20

		switch upper {
		case 'Z':
			if len(current) > 0 {
				lineTo(start)
			}
			closeSub()
			cur = start
		case 'M':
			v, err := sc.numbers(2)
			if err != nil {
				return nil, err
			}
			closeSub()
			cur = point{off.x + v[0], off.y + v[1]}
			start = cur
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
		case 'L':
			v, err := sc.numbers(2)
			if err != nil {
				return nil, err
			}
			lineTo(point{off.x + v[0], off.y + v[1]})
		case 'H':
			v, err := sc.numbers(1)
			if err != nil {
				return nil, err
			}
			lineTo(point{off.x + v[0], cur.y})
		case 'V':
			v, err := sc.numbers(1)
			if err != nil {
				return nil, err
			}
			lineTo(point{cur.x, off.y + v[0]})
		case 'C':
			v, err := sc.numbers(6)
			if err != nil {
				return nil, err
			}
			cubic(point{off.x + v[0], off.y + v[1]}, point{off.x + v[2], off.y + v[3]}, point{off.x + v[4], off.y + v[5]})
		case 'S':
			v, err := sc.numbers(4)
			if err != nil {
				return nil, err
			}
			c1 := cur
			if l := lastCmd &^ 0x20; l == 'C' || l == 'S' {
				c1 = point{2*cur.x - lastCtrl.x, 2*cur.y - lastCtrl.y}
			}
			cubic(c1, point{off.x + v[0], off.y + v[1]}, point{off.x + v[2], off.y + v[3]})
		case 'Q':
			v, err := sc.numbers(4)
			if err != nil {
				return nil, err
			}
			quad(point{off.x + v[0], off.y + v[1]}, point{off.x + v[2], off.y + v[3]})
		case 'T':
			v, err := sc.numbers(2)
			if err != nil {
				return nil, err
			}
			c := cur
			if l := lastCmd &^ 0x20; l == 'Q' || l == 'T' {
				c = point{2*cur.x - lastCtrl.x, 2*cur.y - lastCtrl.y}
			}
			quad(c, point{off.x + v[0], off.y + v[1]})
		case 'A':
			r, err := sc.numbers(3)
			if err != nil {
				return nil, err
			}
			large, err := sc.flag()
			if err != nil {
				return nil, err
			}
			sweep, err := sc.flag()
			if err != nil {
				return nil, err
			}
			e, err := sc.numbers(2)
			if err != nil {
				return nil, err
			}
			for _, q := range arcPoints(cur, r[0], r[1], r[2], large, sweep, point{off.x + e[0], off.y + e[1]}) {
				lineTo(q)
			}
		default:
			return nil, fmt.Errorf("unsupported path command %q", cmd)
		}
		lastCmd = cmd
	}
	closeSub()
	return subpaths, nil
}

func arcPoints(p0 point, rx, ry, phiDeg float64, large, sweep bool, p1 point) []point {
	if p0 == p1 {
		return nil
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return []point{p1}
	}
	phi := phiDeg * math.Pi / 180
	cosP, sinP := math.Cos(phi), math.Sin(phi)
	dx, dy := (p0.x-p1.x)/2, (p0.y-p1.y)/2
	x1p := cosP*dx + sinP*dy
	y1p := -sinP*dx + cosP*dy
	if lambda := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry); lambda > 1 {
		s := math.Sqrt(lambda)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	coef := 0.0
	if den > 0 && num > 0 {
		coef = math.Sqrt(num / den)
	}
	if large == sweep {
		coef = -coef
	}
	cxp := coef * rx * y1p / ry
	cyp := -coef * ry * x1p / rx
	cx := cosP*cxp - sinP*cyp + (p0.x+p1.x)/2
	cy := sinP*cxp + cosP*cyp + (p0.y+p1.y)/2

	angle := func(ux, uy, vx, vy float64) float64 {
		return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	}
	theta1 := angle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	delta := angle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && delta > 0 {
		delta -= 2 * math.Pi
	} else if sweep && delta < 0 {
		delta += 2 * math.Pi
	}

	steps := int(math.Ceil(math.Abs(delta) / (math.Pi / 12)))
	if steps < 2 {
		steps = 2
	}
	out := make([]point, 0, steps)
	for i := 1; i <= steps; i++ {
		t := theta1 + delta*float64(i)/float64(steps)
		x, y := rx*math.Cos(t), ry*math.Sin(t)
		out = append(out, point{cosP*x - sinP*y + cx, sinP*x + cosP*y + cy})
	}
	out[len(out)-1] = p1
	return out
}

type edge struct{ x0, y0, x1, y1 float64 }

func evenOddShape(subpaths [][]point, viewBox, size, offset float64) shape {
	var edges []edge
	for _, sp := range subpaths {
		for i := range sp {
			a, b := sp[i], sp[(i+1)%len(sp)]
			if a.y != b.y {
				edges = append(edges, edge{a.x, a.y, b.x, b.y})
			}
		}
	}
	scale := viewBox / size
	return func(x, y float64) float64 {
		px, py := (x-offset)*scale, (y-offset)*scale
		if px < 0 || py < 0 || px > viewBox || py > viewBox {
			return 0
		}
		in := false
		for _, e := range edges {
			if (e.y0 > py) != (e.y1 > py) {
				if px < e.x0+(py-e.y0)*(e.x1-e.x0)/(e.y1-e.y0) {
					in = !in
				}
			}
		}
		return inside(in)
	}
}

func logoShape(paths []string) (shape, error) {
	var layers []shape
	for _, d := range paths {
		sub, err := parseSVGPath(d)
		if err != nil {
			return nil, err
		}
		layers = append(layers, evenOddShape(sub, 24, 15, 0.5))
	}
	return union(layers...), nil
}
