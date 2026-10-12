package lang

type (
	bailout struct{}
	abort   struct{}
)

func Bail() { panic(bailout{}) }

func Abort() { panic(abort{}) }

func Run(parse func()) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(abort); !ok {
				panic(r)
			}
		}
	}()
	parse()
}

func Attempt(parse func()) (ok bool) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if _, isBailout := r.(bailout); !isBailout {
			panic(r)
		}
		ok = false
	}()
	parse()
	return true
}
