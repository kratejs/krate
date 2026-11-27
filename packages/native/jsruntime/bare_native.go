//go:build !js

package jsruntime

func (r *Runtime) installBare() error {
	s, err := prelude()
	if err != nil {
		return err
	}
	if err := r.evalModule(s.core); err != nil {
		return err
	}
	for _, name := range bareFeatures {
		if err := r.loadFeature(name); err != nil {
			return err
		}
	}
	return r.evalModule(bareModule)
}
