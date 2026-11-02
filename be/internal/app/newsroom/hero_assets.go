package newsroom

import _ "embed"

// Brand and face for the channel-news hero card. Vazirmatn is the product typeface (doc 13 §3);
// the roll is brand/sushi.png, copied here so the card does not reach outside this package.

//go:embed assets/sushi-logo.png
var sushiLogoPNG []byte

//go:embed assets/Vazirmatn-Regular.ttf
var vazirmatnRegularTTF []byte

//go:embed assets/Vazirmatn-Bold.ttf
var vazirmatnBoldTTF []byte
