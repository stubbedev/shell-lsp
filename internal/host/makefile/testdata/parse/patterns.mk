objs := a.o b.o

all: $(objs)

a.o b.o: %.o: %.c
	@echo build $@ from $<

%.c:
	@touch $@

vpath %.h include

.SUFFIXES:
.DELETE_ON_ERROR:
export
