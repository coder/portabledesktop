package desktop

import (
	"reflect"
	"testing"
)

func TestBuildXvncArgs_DisablesSendPrimaryByDefault(t *testing.T) {
	t.Parallel()

	got := buildXvncArgs(7, "1600x900", 24, 96, 5907, true, []string{"-foo"})
	want := []string{
		":7",
		"-geometry", "1600x900",
		"-depth", "24",
		"-dpi", "96",
		"-rfbport", "5907",
		"-SecurityTypes", "None",
		"-ac",
		"-nolisten", "tcp",
		"-localhost", "no",
		"-AcceptSetDesktopSize=1",
		"-SendPrimary=0",
		"-foo",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildXvncArgs() = %v, want %v", got, want)
	}
}
