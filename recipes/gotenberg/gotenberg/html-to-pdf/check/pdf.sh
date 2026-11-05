set -eu
pdf=/outputs/document.pdf
qpdf --check "$pdf" >/dev/null
pages=$(qpdf --show-npages "$pdf")
test "$pages" -ge 1
if [ "$1" = sample-check ]; then
    test "$pages" -eq 2
    pdftk "$pdf" dump_data_utf8 > /tmp/printed-info
    awk '/^PageMediaDimensions:/ { if ($2 < 593 || $2 > 598 || $3 < 418 || $3 > 422) exit 1; pages++ } END { if (pages != 2) exit 1 }' /tmp/printed-info
    grep -Fqx 'InfoValue: Dock manifest' /tmp/printed-info
fi
