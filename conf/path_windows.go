/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2019 WireGuard LLC. All Rights Reserved.
 */

package conf

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

//sys	coTaskMemFree(pointer uintptr) = ole32.CoTaskMemFree
//sys	shGetKnownFolderPath(id *windows.GUID, flags uint32, token windows.Handle, path **uint16) (ret error) = shell32.SHGetKnownFolderPath
//sys	getFileSecurity(fileName *uint16, securityInformation uint32, securityDescriptor *byte, descriptorLen uint32, requestedLen *uint32) (err error) = advapi32.GetFileSecurityW
//sys	getSecurityDescriptorOwner(securityDescriptor *byte, sid **windows.SID, ownerDefaulted *bool) (err error) = advapi32.GetSecurityDescriptorOwner

var folderIDLocalAppData = windows.GUID{0xf1b32785, 0x6fba, 0x4fcf, [8]byte{0x9d, 0x55, 0x7b, 0x8e, 0x7f, 0x15, 0x70, 0x91}}

const kfFlagCreate = 0x00008000
const ownerSecurityInformation = 0x00000001

var cachedConfigFileDir string
var cachedRootDir string

func maybeMigrate(c string) {
	vol := filepath.VolumeName(c)
	withoutVol := strings.TrimPrefix(c, vol)
	oldRoot := filepath.Join(vol, "\\windows.old")
	oldC := filepath.Join(oldRoot, withoutVol)

	var err error
	var sd []byte
	reqLen := uint32(128)
	for {
		sd = make([]byte, reqLen)
		//XXX: Since this takes a file path, it's technically a TOCTOU.
		err = getFileSecurity(windows.StringToUTF16Ptr(oldRoot), ownerSecurityInformation, &sd[0], uint32(len(sd)), &reqLen)
		if err != windows.ERROR_INSUFFICIENT_BUFFER {
			break
		}
	}
	if err == windows.ERROR_PATH_NOT_FOUND {
		return
	}
	if err != nil {
		log.Printf("Not migrating configuration from '%s' due to GetFileSecurity error: %v", oldRoot, err)
		return
	}
	var defaulted bool
	var sid *windows.SID
	err = getSecurityDescriptorOwner(&sd[0], &sid, &defaulted)
	if err != nil {
		log.Printf("Not migrating configuration from '%s' due to GetSecurityDescriptorOwner error: %v", oldRoot, err)
		return
	}
	if defaulted || !sid.IsWellKnown(windows.WinLocalSystemSid) {
		sidStr, _ := sid.String()
		log.Printf("Not migrating configuration from '%s' it is not explicitly owned by SYSTEM, but rather '%s'", oldRoot, sidStr)
		return
	}
	err = windows.MoveFileEx(windows.StringToUTF16Ptr(oldC), windows.StringToUTF16Ptr(c), windows.MOVEFILE_COPY_ALLOWED)
	if err != nil {
		if err != windows.ERROR_FILE_NOT_FOUND && err != windows.ERROR_ALREADY_EXISTS {
			log.Printf("Not migrating configuration from '%s' due to error when moving files: %v", oldRoot, err)
		}
		return
	}
	log.Printf("Migrated configuration from '%s'", oldRoot)
}

func tunnelConfigurationsDirectory() (string, error) {
	if cachedConfigFileDir != "" {
		return cachedConfigFileDir, nil
	}
	root, err := RootDirectory()
	if err != nil {
		return "", err
	}
	c := filepath.Join(root, "Configurations")
	maybeMigrate(c)
	err = os.MkdirAll(c, os.ModeDir|0700)
	if err != nil {
		return "", err
	}
	cachedConfigFileDir = c
	return cachedConfigFileDir, nil
}

func RootDirectory() (string, error) {
	if cachedRootDir != "" {
		return cachedRootDir, nil
	}
	var path *uint16
	err := shGetKnownFolderPath(&folderIDLocalAppData, kfFlagCreate, 0, &path)
	if err != nil {
		return "", err
	}
	defer coTaskMemFree(uintptr(unsafe.Pointer(path)))
	root := windows.UTF16ToString((*[windows.MAX_LONG_PATH + 1]uint16)(unsafe.Pointer(path))[:])
	if len(root) == 0 {
		return "", errors.New("Unable to determine configuration directory")
	}
	c := filepath.Join(root, "WireGuard")
	err = os.MkdirAll(c, os.ModeDir|0700)
	if err != nil {
		return "", err
	}
	cachedRootDir = c
	return cachedRootDir, nil
}
