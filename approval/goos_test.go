package approval

import "runtime"

func goosIsLinux() bool { return runtime.GOOS == "linux" }
