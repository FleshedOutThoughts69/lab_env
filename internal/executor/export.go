package executor

import "io/fs"

// CanonicalFile is the exported view of a canonical file entry.
// Used by tests to verify embedded content, mode, and ownership.
type CanonicalFile struct {
    Content []byte
    Mode    fs.FileMode
    Owner   string
    Group   string
}

// CanonicalFileEntry returns the canonical file entry for the given path.
// Returns false if the path is not in the canonical map.
// Used by tests to verify embedded content without going through RestoreFile.
func CanonicalFileEntry(path string) (CanonicalFile, bool) {
    f, ok := canonicalFiles[path]
    if !ok {
        return CanonicalFile{}, false
    }
    return CanonicalFile{
        Content: f.content,
        Mode:    f.mode,
        Owner:   f.owner,
        Group:   f.group,
    }, true
}