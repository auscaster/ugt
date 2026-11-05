set -eu
cd "$(mktemp -d)"
api=http://gotenberg:3000/forms
printf '%s\n' '<title>Workshop pack</title><link rel="stylesheet" href="print.css"><p>Valve</p><p>Gasket</p>' > index.html
printf '%s\n' 'p + p {break-before:page}' > print.css
curl --fail --silent --show-error --max-time 60 \
    --form files=@index.html --form files=@print.css --form failOnResourceLoadingFailed=true \
    "$api/chromium/convert/html" --output printed.pdf
qpdf --check printed.pdf >/dev/null
test "$(qpdf --show-npages printed.pdf)" -eq 2

if [ "${1:-}" = sample ]; then
    cp printed.pdf second.pdf
    curl --fail --silent --show-error --max-time 60 \
        --form files=@printed.pdf --form files=@second.pdf \
        "$api/pdfengines/merge" --output merged.pdf
    qpdf --check merged.pdf >/dev/null
    test "$(qpdf --show-npages merged.pdf)" -eq 4
fi
