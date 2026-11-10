// Package privilege checks for and obtains the administrator rights needed
// to write raw devices.
package privilege

import "errors"

// ErrRelaunched is returned by Elevate on Windows once an elevated copy of
// the program was started in a new window; the current process should exit.
var ErrRelaunched = errors.New("relaunched with administrator rights in a new window")

// ElevatedFlag is appended to the arguments of a relaunched Windows process
// so it can keep its window open after a fatal error.
const ElevatedFlag = "--elevated"
