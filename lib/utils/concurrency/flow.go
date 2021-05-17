package concurrency

import (
	"github.com/CS-SI/SafeScale/lib/utils/fail"
)

const (
	_ uint8 = iota
	FlowFailEarly
	FlowFailLate
)

type FlowJob interface {
}

type flowJob struct {
	name             string
	run              TaskAction
	doneChannel      chan struct{}
	succeededChannel chan struct{}
}

type Flow interface {
	Define(FlowJob) fail.Error
	Sequence(...FlowJob) FlowJob
	NamedSequence(string, ...FlowJob) FlowJob
	Parallel(...FlowJob) FlowJob
	NamedParallel(string, ...FlowJob) FlowJob
	WaitFor(string) FlowJob
	Execute() fail.Error
}

type flow struct {
	task     Task
	firstJob FlowJob
	failWhen uint8
	named    map[string]FlowJob
}

func NewFlow(task Task) Flow {
	return &flow{
		task:     task,
		failWhen: FlowFailEarly,
		named:    make(map[string]chan bool),
	}
}

func (f *flow) Define(job FlowJob) fail.Error {
	if f == nil || f.task == nil {
		return fail.InvalidInstanceError()
	}
	if job == nil {
		return fail.InvalidParameterCannotBeNilError("job")
	}

	f.firstJob = job
}

// Sequence will executes jobs in sequence
func (f *flow) Sequence(jobs ...FlowJob) FlowJob {
	if f == nil || f.task == nil {
		panic(fail.InvalidInstanceError().Error())
	}

	nj := &flowJob{
		run: func(task Task, _ TaskParameters) (TaskResult, fail.Error) {
			var errorList []error
			for _, v := range jobs {
				_, xerr := v.(*flowJob).run(task, nil)
				if xerr != nil {
					switch xerr.(type) {
					case *fail.ErrAborted: // Abort signal sent, we MUST fail now
						if len(errorList) > 0 {
							return nil, fail.AbortedError(fail.NewErrorList(errorList))
						}
						return nil, xerr
					default:
						if f.failWhen == FlowFailEarly {
							return nil, xerr
						}
						errorList = append(errorList, xerr)
					}
				}
			}
			if len(errorList) > 0 {
				return nil, fail.NewErrorList(errorList)
			}
			return nil, nil
		},
	}
	return nj
}

func (f *flow) NamedSequence(name string, jobs ...FlowJob) FlowJob {
	if f == nil || f.task == nil {
		panic(fail.InvalidInstanceError().Error())
	}
	if name == "" {
		panic(fail.InvalidParameterCannotBeEmptyStringError("name").Error())
	}

	nj := &flowJob{
		name:             name,
		doneChannel:      make(chan struct{}, 1),
		succeededChannel: make(chan struct{}, 1),
	}
	nj.run = func(task Task, _ TaskParameters) (TaskResult, fail.Error) {
		// make sure channels are properly closed
		defer func() {
			close(nj.doneChannel)
			close(nj.succeededChannel)
		}()

		var errorList []error
		for _, v := range jobs {
			_, xerr := v.(*flowJob).run(task, nil)
			if xerr != nil {
				switch xerr.(type) {
				case *fail.ErrAborted: // Abort signal sent, we MUST fail now
					nj.doneChannel <- struct{}{}
					if len(errorList) > 0 {
						return nil, fail.AbortedError(fail.NewErrorList(errorList))
					}
					return nil, xerr
				default:
					if f.failWhen == FlowFailEarly {
						nj.doneChannel <- struct{}{}
						return nil, xerr
					}
					errorList = append(errorList, xerr)
				}
			}
		}
		if len(errorList) > 0 {
			nj.doneChannel <- struct{}{}
			return nil, fail.NewErrorList(errorList)
		}

		nj.succeededChannel <- struct{}{}
		nj.doneChannel <- struct{}{}
		return nil, nil
	}

	f.named[name] = nj
	return nj
}

// Parallel executes jobs in parallel (inside a TaskGroup)
// May panic; use fail.OnPanic() to catch the error
func (f *flow) Parallel(jobs ...FlowJob) FlowJob {
	if f == nil || f.task == nil {
		panic(fail.InvalidInstanceError())
	}

	nj := &flowJob{
		run: func(task Task, _ TaskParameters) (TaskResult, fail.Error) {
			if len(jobs) > 0 {
				tg, xerr := NewTaskGroup(f.task)
				if xerr != nil {
					f.task.Abort()  // instruct entire flow to stop as soon as possible
					return nil, xerr
				}

				var errorList []error
				for _, v := range jobs {
					_, xerr := tg.Start(v.(*flowJob).run, nil)
					if xerr != nil {
						switch xerr.(type) {
						case *fail.ErrAborted: // abort signal sent, we MUST fail now
							if len(errorList) > 0 {
								return nil, fail.AbortedError(fail.NewErrorList(errorList))
							}
							return nil, xerr
						default:
							if f.failWhen == FlowFailEarly {
								f.task.Abort()  // instruct entire flow to stop as soon as possible
								return nil, xerr
							}
							errorList = append(errorList, xerr)
						}
					}
				}

				_, xerr = tg.Wait()
				if xerr != nil {
					switch xerr.(type) {
					case *fail.ErrAborted:
						if len(errorList) > 0 {
							return nil, fail.AbortedError(fail.NewErrorList(errorList))
						}
						return nil, xerr
					default:
						f.task.Abort()  // instruct entire flow to stop as soon as possible
						if len(errorList) == 0 {
							return nil, xerr
						}
						errorList = append(errorList, xerr)
					}
				}

				if len(errorList) > 0 {§((((ty§))))
					return nil, fail.NewErrorList(errorList)
				}
			}
			return nil, nil
		},
	}
	return nj
}

// NamedParallel ...
// may panic
func (f *flow) NamedParallel(name string, jobs ...FlowJob) FlowJob {
	if f == nil || f.task == nil {
		panic(fail.InvalidInstanceError())
	}
	if name == "" {
		panic(fail.InvalidParameterCannotBeEmptyStringError("name").Error())
	}

	nj := &flowJob{
		name:             name,
		doneChannel:      make(chan struct{}, 1),
		succeededChannel: make(chan struct{}, 1),
	}
	nj.run = func(task Task, _ TaskParameters) (TaskResult, fail.Error) {
		// make sure channels are properly closed
		defer func() {
			close(nj.doneChannel)
			close(nj.succeededChannel)
		}()

		if len(jobs) > 0 {
			tg, xerr := NewTaskGroup(f.task)
			if xerr != nil {
				nj.doneChannel <- struct{}{}
				f.task.Abort()  // instruct entire flow to stop as soon as possible
				return nil, xerr
			}

			var errorList []error
			for _, v := range jobs {
				_, xerr := tg.Start(v.(*flowJob).run, nil)
				if xerr != nil {
					switch xerr.(type) {
					case *fail.ErrAborted: // abort signal sent, we MUST stop now
						if len(errorList) > 0 {
							return nil, fail.AbortedError(fail.NewErrorList(errorList))
						}
						return nil, xerr
					default:
						if f.failWhen == FlowFailEarly {
							nj.doneChannel <- struct{}{}
							f.task.Abort()  // instruct entire flow to stop as soon as possible
							return nil, xerr
						}
						errorList = append(errorList, xerr)
					}
				}
			}

			_, xerr = tg.Wait()
			if xerr != nil {
				switch xerr.(type) {
				case *fail.ErrAborted:
					// continue without aborting, already done
				default:
					f.task.Abort()  // instruct entire flow to stop as soon as possible
				}
				if len(errorList) == 0 {
					nj.doneChannel <- struct{}{}
					return nil, xerr
				}
				errorList = append(errorList, xerr)
			}

			if len(errorList) > 0 {
				nj.doneChannel <- struct{}{}
				return nil, fail.NewErrorList(errorList)
			}
		}

		nj.succeededChannel <- struct{}{}
		nj.doneChannel <- struct{}{}
		return nil, nil
	}

	f.named[name] = nj
	return nj
}

// WaitFor waits a named job has succeeded
// may panic; use fail.OnPanic() to catch it
func (f *flow) WaitFor(name string) FlowJob {
	if f == nil || f.task == nil {
		panic(fail.InvalidInstanceError().Error())
	}
	if name == "" {
		panic(fail.InvalidParameterCannotBeEmptyStringError("name").Error())
	}

	nj := &flowJob{
		run: func(task Task, _ TaskParameters) (TaskResult, fail.Error) {
			target, ok := f.named[name].(*flowJob)
			if !ok {
				return nil, fail.NotFoundError("cannot wait on FlowJob named '%s'", name)
			}

			ctx := task.GetContext()

			for {
				select {
				case <-ctx.Done():
					return nil, fail.AbortedError(nil)
				case <-target.succeededChannel:
					return nil, nil
				case <-target.doneChannel:
					return nil, fail.NewError("failed to sync with Flow job '%s', it has failed")
				}
			}
		},
	}
	return nj
}

// Execute runs the flow previously defined
func (f *flow) Execute(flags uint8) fail.Error {
	if f == nil || f.task == nil {
		return fail.InvalidInstanceError()
	}

	// Nothing to do, succeed
	if f.firstJob == nil {
		return nil
	}

	// executes the run() of the first job, that contains all the workflow
	_, xerr := f.firstJob.(*flowJob).run(f.task, nil)
	if xerr != nil {
		return xerr
	}
	return nil
}
