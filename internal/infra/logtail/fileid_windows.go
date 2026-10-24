//go:build windows

package logtail

import (
	"fmt"
	"os"
	"syscall"
)

// openShared opens with FILE_SHARE_DELETE (plus read/write). Without it the
// retained handle would make the writer's rotation rename fail — the
// rotation fix would turn lost lines into a broken rotation (OLD C-C2).
func openShared(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// fileKey identifies the file behind f across renames: volume serial +
// file index.
func fileKey(f *os.File) (string, error) {
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}

// pathKey identifies the file currently at path (opened briefly, shared).
func pathKey(path string) (string, error) {
	f, err := openShared(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return fileKey(f)
}
