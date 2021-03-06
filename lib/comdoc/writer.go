//
// Copyright (c) SAS Institute Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package comdoc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
)

// Add or replace a named stream with the given contents. Only streams within
// the root storage are currently supported.
func (r *ComDoc) AddFile(name string, contents []byte) error {
	if r.writer == nil {
		return errors.New("file is not open for writing")
	}
	if err := r.DeleteFile(name); err != nil {
		return err
	}
	// store contents
	isShort := len(contents) < int(r.Header.MinStdStreamSize)
	nextSector, err := r.addStream(contents, isShort)
	if err != nil {
		return err
	}
	// create new dirent
	dirent, err := r.newDirEnt(name, uint32(len(contents)), nextSector)
	if err != nil {
		return err
	}
	r.rootFiles = append(r.rootFiles, dirent.Index)
	r.changed = true
	return nil
}

// Delete a file from the root storage if it exists
func (r *ComDoc) DeleteFile(name string) error {
	keepFiles := make([]int, 0, len(r.rootFiles))
	for _, index := range r.rootFiles {
		item := &r.Files[index]
		if !strings.EqualFold(item.name, name) {
			keepFiles = append(keepFiles, index)
			continue
		}
		if item.Type != DirStream {
			return errors.New("can't delete or replace storages")
		}
		// free storage
		if item.StreamSize < r.Header.MinStdStreamSize {
			freeSectors(r.SSAT, item.NextSector)
		} else {
			freeSectors(r.SAT, item.NextSector)
		}
		// blank out the dirent
		*item = DirEnt{}
		r.changed = true
	}
	r.rootFiles = keepFiles
	return nil
}

// Close the CDF and, if open for writing, commit the remainder of structures
// to disk.
func (r *ComDoc) Close() error {
	if !r.changed {
		if r.closer != nil {
			r.closer.Close()
			r.closer = nil
		}
		return nil
	}
	if err := r.writeShortSAT(); err != nil {
		return err
	}
	if err := r.writeDirStream(); err != nil {
		return err
	}
	// Write MSAT and SAT
	satList, msatList := r.allocSectorTables()
	if err := r.writeSAT(satList); err != nil {
		return err
	}
	if err := r.writeMSAT(satList, msatList); err != nil {
		return err
	}
	// Write file header
	copy(r.Header.Magic[:], fileMagic)
	r.Header.ByteOrder = byteOrderMarker
	r.Header.SATSectors = uint32(len(satList))
	r.Header.MSATSectorCount = uint32(len(msatList))
	buf := bytes.NewBuffer(make([]byte, 0, 512))
	binary.Write(buf, binary.LittleEndian, r.Header)
	if _, err := r.writer.WriteAt(buf.Bytes(), 0); err != nil {
		return err
	}
	if r.closer != nil {
		r.closer.Close()
		r.closer = nil
	}
	return nil
}
