package errcontrol

func yeah() error {
	return nil
}

func checkYeah() error {
	rErr := yeah()
	rErr = Crasher(rErr)
	return rErr
}

func checkYeahUntouched() error {
	rErr := yeah()
	return rErr
}
