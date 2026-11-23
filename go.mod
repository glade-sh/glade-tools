module github.com/glade-sh/glade/tools

go 1.27.4

require (
	github.com/glade-sh/glade v0.0.0
	golang.org/x/mod v0.42.0
	golang.org/x/sys v0.50.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.62.0
)

require (
	github.com/dlclark/metaphone3 v0.0.0-20190903202417-5fe87fcdd547 // indirect
	github.com/dlclark/regexp2 v1.12.0 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/glade-sh/apex-parser v0.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/tree-sitter/go-tree-sitter v0.25.0 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace github.com/glade-sh/glade => ../glade

replace github.com/glade-sh/apex-parser => ../glade/third_party/glade-apex-parser
