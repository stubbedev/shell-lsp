define BANNER
@echo ==== $(1) ====
endef

define RULE =
$(1):
	@echo $(1)
endef

all: one two ; @echo inline

one:
	$(call BANNER,one)
	touch $@

two: | one
	@echo two

three four: one
	@echo $@

debug: MODE = debug
debug: CFLAGS += -g
debug:
	@echo $(MODE)

multi::
	@echo first
multi::
	@echo second
