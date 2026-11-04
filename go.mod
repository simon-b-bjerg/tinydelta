module github.com/simon-b-bjerg/tinydelta

go 1.21

// Patches without a version byte, which later decoders reject.
retract v1.0.0
