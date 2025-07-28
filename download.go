package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

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
func PrepareChallengeFS(challenges []Challenge, downloadRoot string) error {
	os.RemoveAll(downloadRoot)
	for _, ch := range challenges {
		srcDir := "chals/" + strings.ToLower(ch.Title)
		tgtDir := filepath.Join(downloadRoot, ch.Title)
		if err := os.MkdirAll(tgtDir, 0755); err != nil {
			return err
		}
		for _, f := range ch.DownloadFiles {
			srcPath := filepath.Join(srcDir, f)
			dstPath := filepath.Join(tgtDir, f)
			if err := copyFileOrDir(srcPath, dstPath); err != nil {
				log.Printf("Failed to copy %s for challenge %s: %v", f, ch.Title, err)
			}
		}
	}
	return nil
}
