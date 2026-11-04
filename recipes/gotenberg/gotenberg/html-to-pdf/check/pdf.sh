set -eu
pdf=/outputs/document.pdf
qpdf --check "$pdf" >/dev/null
pages=$(qpdf --show-npages "$pdf")
test "$pages" -ge 1
if [ "$1" = sample-check ]; then
    test "$pages" -eq 2
    pdftk "$pdf" dump_data_utf8 | grep -Fqx 'InfoValue: Dock manifest'
fi
