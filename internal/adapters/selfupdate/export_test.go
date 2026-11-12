package selfupdate

// NewFor is New for a given binary instead of the running one.
func NewFor(dist, exe string) *Installer {
	i := New(dist, false)
	i.exe, i.exeErr = exe, nil
	return i
}
