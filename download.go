package main

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/pkg/sftp"
)

type sftpHandler struct {
	root string
}

var (
	_ sftp.FileLister = &sftpHandler{}
	_ sftp.FileReader = &sftpHandler{}
)

type listerAt []fs.FileInfo

func (l listerAt) ListAt(ls []fs.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(ls, l[offset:])
	if n < len(ls) {
		return n, io.EOF
	}
	return n, nil
}

func (s *sftpHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	f, err := os.Open(filepath.Join(s.root, r.Filepath))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *sftpHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		entries, err := os.ReadDir(filepath.Join(s.root, r.Filepath))
		if err != nil {
			return nil, fmt.Errorf("sftp: %w", err)
		}
		infos := make([]fs.FileInfo, len(entries))
		for i, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			infos[i] = info
		}
		return listerAt(infos), nil
	case "Stat":
		fi, err := os.Stat(filepath.Join(s.root, r.Filepath))
		if err != nil {
			return nil, err
		}
		return listerAt{fi}, nil
	default:
		return nil, sftp.ErrSSHFxOpUnsupported
	}
}

func sftpSubsystem(root string) ssh.SubsystemHandler {
	return func(s ssh.Session) {
		fs := &sftpHandler{root}
		srv := sftp.NewRequestServer(s, sftp.Handlers{
			FileList: fs,
			FileGet:  fs,
		})
		if err := srv.Serve(); err == io.EOF {
			_ = srv.Close()
		} else if err != nil {
			wish.Fatalln(s, "sftp:", err)
		}
	}
}

// Recursively copy a file or directory from src to dst
func copyFileOrDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// PrepareChallengeFS copies only the allowed files for each challenge to downloadRoot
func PrepareChallengeFS(challenges map[string]Challenge, downloadRoot string) error {
	os.RemoveAll(downloadRoot)
	for _, ch := range challenges {
		srcDir := challengeDir + "/" + strings.ToLower(ch.Name)
		tgtDir := filepath.Join(downloadRoot, ch.Name)
		if err := os.MkdirAll(tgtDir, 0755); err != nil {
			return err
		}
		for _, f := range ch.Downloads {
			srcPath := filepath.Join(srcDir, f)
			dstPath := filepath.Join(tgtDir, f)
			if err := copyFileOrDir(srcPath, dstPath); err != nil {
				log.Printf("Failed to copy %s for challenge %s: %v", f, ch.Name, err)
			}
		}
	}
	return nil
}
