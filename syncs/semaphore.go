package syncs

type Semaphore chan bool

func NewSemaphore(n int) Semaphore {
	return make(chan bool, n)
}

func (s Semaphore) Acquire() {
	s <- true
}

func (s Semaphore) Release() {
	<-s
}
