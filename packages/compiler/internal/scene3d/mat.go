package scene3d

import "math"

// mat4 is a column-major 4x4 matrix, the layout Filament and the runtime use.
type mat4 [16]float64

var identity = mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}

func (m mat4) isIdentity() bool { return m == identity }

// mul returns a * b.
func mul(a, b mat4) mat4 {
	var out mat4
	for c := 0; c < 4; c++ {
		for r := 0; r < 4; r++ {
			out[c*4+r] = a[r]*b[c*4] + a[4+r]*b[c*4+1] + a[8+r]*b[c*4+2] + a[12+r]*b[c*4+3]
		}
	}
	return out
}

// eulerQuat converts Euler XYZ angles (radians, Three.js order) to a unit
// quaternion [x, y, z, w]; it is the same formula as eulerToQuat in transforms.ts.
func eulerQuat(x, y, z float64) [4]float64 {
	c1, c2, c3 := math.Cos(x/2), math.Cos(y/2), math.Cos(z/2)
	s1, s2, s3 := math.Sin(x/2), math.Sin(y/2), math.Sin(z/2)
	return [4]float64{
		s1*c2*c3 + c1*s2*s3,
		c1*s2*c3 - s1*c2*s3,
		c1*c2*s3 + s1*s2*c3,
		c1*c2*c3 - s1*s2*s3,
	}
}

// composeTRS builds T * R * S like composeTRS in transforms.ts.
func composeTRS(pos [3]float64, q [4]float64, s [3]float64) mat4 {
	x, y, z, w := q[0], q[1], q[2], q[3]
	x2, y2, z2 := x+x, y+y, z+z
	xx, xy, xz := x*x2, x*y2, x*z2
	yy, yz, zz := y*y2, y*z2, z*z2
	wx, wy, wz := w*x2, w*y2, w*z2
	return mat4{
		(1 - (yy + zz)) * s[0], (xy + wz) * s[0], (xz - wy) * s[0], 0,
		(xy - wz) * s[1], (1 - (xx + zz)) * s[1], (yz + wx) * s[1], 0,
		(xz + wy) * s[2], (yz - wx) * s[2], (1 - (xx + yy)) * s[2], 0,
		pos[0], pos[1], pos[2], 1,
	}
}

// transformPoint applies the matrix to a point (w = 1).
func (m mat4) transformPoint(p [3]float64) [3]float64 {
	return [3]float64{
		m[0]*p[0] + m[4]*p[1] + m[8]*p[2] + m[12],
		m[1]*p[0] + m[5]*p[1] + m[9]*p[2] + m[13],
		m[2]*p[0] + m[6]*p[1] + m[10]*p[2] + m[14],
	}
}

func normalize3(v [3]float64) [3]float64 {
	l := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
	if l == 0 {
		return v
	}
	return [3]float64{v[0] / l, v[1] / l, v[2] / l}
}
