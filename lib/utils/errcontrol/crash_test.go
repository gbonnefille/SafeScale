package errcontrol

import "testing"

func TestWithContext(t *testing.T) {
	err := CrashSetup("crash_test_helper.go:9:1") // Line 9 of crash_test_helper will fail with a probability of 100% (1)
	if err != nil {
		t.FailNow()
	}

	broken := checkYeah()
	if broken == nil {
		t.FailNow()
	}

	unbroken := checkYeahUntouched()
	if unbroken != nil {
		t.FailNow()
	}
}

func TestWithEmptyContext(t *testing.T) {
	err := CrashSetup("")
	if err != nil {
		t.FailNow()
	}

	broken := checkYeah()
	if broken != nil {
		t.Errorf(broken.Error())
		t.FailNow()
	}

	unbroken := checkYeahUntouched()
	if unbroken != nil {
		t.Errorf(unbroken.Error())
		t.FailNow()
	}
}
