package download

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

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

func SftpSubsystem(root string) ssh.SubsystemHandler {
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


