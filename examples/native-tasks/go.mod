module example.com/native-tasks

go 1.26.1

require github.com/kratejs/krate/packages/native v0.0.0

// Inside the Krate monorepo the host module is the sibling package. An app
// outside it drops this line and runs: go get github.com/kratejs/krate/packages/native
replace github.com/kratejs/krate/packages/native => ../../packages/native
