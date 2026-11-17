.PHONY: regression regression-update test

# Run the pixel regression suite: every filter x fixed input image,
# PNG output hashed and compared against testdata/regression_hashes.json.
regression:
	go test -count=1 -run TestPixelRegression -v .

# Regenerate the committed golden hashes after an intentional filter change.
regression-update:
	go test -count=1 -run TestPixelRegression . -update

test:
	go test -count=1 ./...
