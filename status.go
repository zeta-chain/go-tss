package tss

type Status byte

const (
	Success Status = iota
	Fail
	NA
)
