package maincmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

func canUnexpectedlyWriteTo(dir string) error {
	fn := filepath.Join(dir, "gokr-rsyncd.unexpectedly_writable")
	if err := ioutil.WriteFile(fn, []byte("gokr-rsyncd creates this file to prevent misconfigurations. if you see this file, it means gokr-rsyncd unexpectedly was started with too many privileges"), 0644); err == nil {
		os.Remove(fn)
		return fmt.Errorf("unexpectedly able to write file to %s, exiting", dir)
	}
	return nil
}
