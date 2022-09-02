# LMOtelFY

Let Me OTel(Open Telemetry) For You

### What this program does?
It will look for two situations:
 - Exported functions that has `context.Cotext` as a parameter.
 - HTTP handlers
 
And create:
- `Start()` new span made by "`package`.`function name`".
- defer the span `End()`
- call `span.RecordError(err)` when error is handled in the function.
- Add the imports needed

##### Examples
This code:
```go
import (
	"context"
	"errors"
	"log"
	"net/http"
)

func H(w http.ResponseWriter, r *http.Request) {
	 ...
	if err != nil {
		log.Println(err.Error())
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
 ...
}

func C(ctx context.Context) error {
	...
	if err != nil {
		return err
	}
	...
}
```

will turn into this:
```go 
import (
	"context"
	"errors"
	"github.com/example/extensions/telemetry" // New import needed
	"log"
	"net/http"
)

func H(w http.ResponseWriter, r *http.Request) {
	ctx, span := telemetry.FromContext(ctx).Start(ctx, "samples.H")
	defer span.End()
	...
	if err != nil {
		span.RecordError(err)
		log.Println(err.Error())
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
  ...
}

func C(ctx context.Context) error {
	ctx, span := telemetry.FromContext(ctx).Start(ctx, "samples.C")
	defer span.End()
	...
	if err != nil {
		span.RecordError(err)
		return err
	}
  ...
}
```

### Params
- `--dry-run`
  - when dry run is true the program will modify the files found
  - default: true
- `--path`
  - What path to read?
  - default: "."
- `--import`
  - Where is your telemetry functions?
  - default: "github.com/example/extensions/telemetry"
- `--allowed-dirs`
  - The list of dir fragments allowed
  - default: "samples"
- `--disallowed-dirs`
  - The list of dir fragments disallowed
  - default: "."