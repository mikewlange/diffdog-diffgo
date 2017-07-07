.PHONY: all
all: ; @go build httpdiff.go

.PHONY: install
install: ; @go install C:\Users\mike\go\bin

.PHONY: clean
clean: ; @rm httpdiff
