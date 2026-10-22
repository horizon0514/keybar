//go:build windows

package clip

import "time"

func syscallSleep() { time.Sleep(20 * time.Millisecond) }
