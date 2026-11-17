package library

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pandaCaffeine/diadem/internal/logx"
)

// Scan walks rootPath and builds the Library tree:
//
//   - dot-prefixed files and folders are hidden;
//   - symlinks and special files are skipped (v1 does not follow symlinks);
//   - files whose extension is not media are skipped;
//   - Containers left without any Item in their subtree are pruned.
//
// Children are sorted: Containers first, then natural sort by name.
// Video Items get their container metadata (title, duration) probed; a
// Store supplies the cross-scan probe cache via scanStore.
func Scan(rootPath string) (*Library, error) {
	return scanStore(rootPath, nil, nil)
}

// scanStore is Scan with the Store's probe cache and logger attached.
func scanStore(rootPath string, cache *probeCache, logger logx.Logger) (*Library, error) {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", abs)
	}

	root := &Node{Name: filepath.Base(abs), Path: abs, Kind: Container}
	stack := []*Node{root}

	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == abs {
				return err // the Library root itself is unreadable
			}
			return nil // skip unreadable entries, keep scanning
		}
		if path == abs {
			return nil // the root is already on the stack
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Pop the stack until it holds the parent of this path.
		parentDir := filepath.Dir(path)
		for len(stack) > 1 && stack[len(stack)-1].Path != parentDir {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1]

		switch {
		case d.IsDir():
			child := &Node{Name: d.Name(), Path: path, Kind: Container}
			parent.Children = append(parent.Children, child)
			stack = append(stack, child)
		case d.Type().IsRegular():
			kind, ok := KindByExt(d.Name())
			if !ok {
				return nil
			}
			size := int64(0)
			if fi, ierr := d.Info(); ierr == nil {
				size = fi.Size()
			}
			parent.Children = append(parent.Children, &Node{
				Name: d.Name(), Path: path, Kind: Item, Media: kind, Size: size,
			})
		default:
			return nil // symlink, device, socket, ...
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	prune(root)
	sortNodes(root)

	probeVideos(root, cache, logger)

	lib := &Library{Root: root}
	collect(root, lib)
	return lib, nil
}

// prune drops Containers with no Items in their subtree. The root always
// survives, even when empty.
func prune(n *Node) bool {
	if n.Kind == Item {
		return true
	}
	kept := n.Children[:0]
	for _, c := range n.Children {
		if prune(c) {
			kept = append(kept, c)
		}
	}
	n.Children = kept
	return len(kept) > 0
}

func sortNodes(n *Node) {
	for _, c := range n.Children {
		if c.Kind == Container {
			sortNodes(c)
		}
	}
	sort.SliceStable(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		if a.Kind != b.Kind {
			return a.Kind == Container
		}
		return NaturalLess(a.Name, b.Name)
	})
}

func collect(n *Node, lib *Library) {
	if n.Kind == Item {
		lib.Items++
		lib.Bytes += n.Size
		return
	}
	if n != lib.Root {
		lib.Containers++
	}
	for _, c := range n.Children {
		collect(c, lib)
	}
}
