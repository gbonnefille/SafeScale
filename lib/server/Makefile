include ../../common.mk

.PHONY:	lib/server/resources lib/server/iaas clean vet

all:	lib/server/iaas lib/server/resources 

generate: clean
	@(mkdir -p mocks) || true
	@(cd iaas && $(MAKE) $@)
	@(cd resources && $(MAKE) $@)
	@(cd handlers && $(MAKE) $@)

vet:
	@$(GO) vet ./...

test:
	@$(GO) test $(RACE_CHECK_TEST) -v ./...

lib/server/iaas:
	@printf "%b" "$(MAKE_TRACE)$(OK_COLOR)$(INFO_STRING) Building component iaas, $(NO_COLOR)target $(OBJ_COLOR)$(@)$(NO_COLOR)\n";
	@(cd iaas && $(MAKE))

lib/server/resources:
	@printf "%b" "$(MAKE_TRACE)$(OK_COLOR)$(INFO_STRING) Building component resources, $(NO_COLOR)target $(OBJ_COLOR)$(@)$(NO_COLOR)\n";
	@(cd resources && $(MAKE))

lib/server/handlers:
	@printf "%b" "$(MAKE_TRACE)$(OK_COLOR)$(INFO_STRING) Building component handlers, $(NO_COLOR)target $(OBJ_COLOR)$(@)$(NO_COLOR)\n";
	@(cd handlers && $(MAKE))

clean:
	@(cd iaas && $(MAKE) $@)
	@(cd resources && $(MAKE) $@)
	@$(RM) ./mocks/*.go || true
