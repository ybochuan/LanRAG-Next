//go:build !windows

package main

func prepareInstalledDesktop(string) (*installedDesktop, error) { return nil, nil }
