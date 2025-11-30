package tailang

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func RegisterStdLib(env *Env) {
	register(env,
		// fmt
		fmt.Print,
		fmt.Printf,
		fmt.Println,
		fmt.Sprint,
		fmt.Sprintf,
		fmt.Sprintln,
		fmt.Errorf,

		// strings
		strings.Contains,
		strings.ContainsAny,
		strings.ContainsRune,
		strings.Count,
		strings.Cut,
		strings.EqualFold,
		strings.Fields,
		strings.HasPrefix,
		strings.HasSuffix,
		strings.Index,
		strings.IndexAny,
		strings.IndexByte,
		strings.IndexRune,
		strings.Join,
		strings.LastIndex,
		strings.LastIndexAny,
		strings.LastIndexByte,
		strings.Map,
		strings.Repeat,
		strings.Replace,
		strings.ReplaceAll,
		strings.Split,
		strings.SplitAfter,
		strings.SplitAfterN,
		strings.SplitN,
		strings.Title,
		strings.ToLower,
		strings.ToLowerSpecial,
		strings.ToTitle,
		strings.ToTitleSpecial,
		strings.ToUpper,
		strings.ToUpperSpecial,
		strings.ToValidUTF8,
		strings.Trim,
		strings.TrimFunc,
		strings.TrimLeft,
		strings.TrimLeftFunc,
		strings.TrimPrefix,
		strings.TrimRight,
		strings.TrimRightFunc,
		strings.TrimSpace,
		strings.TrimSuffix,
		strings.Clone,
		strings.Compare,

		// bytes
		bytes.Compare,
		bytes.Contains,
		bytes.ContainsAny,
		bytes.ContainsRune,
		bytes.Count,
		bytes.Cut,
		bytes.Equal,
		bytes.EqualFold,
		bytes.Fields,
		bytes.HasPrefix,
		bytes.HasSuffix,
		bytes.Index,
		bytes.IndexAny,
		bytes.IndexByte,
		bytes.IndexRune,
		bytes.Join,
		bytes.LastIndex,
		bytes.LastIndexAny,
		bytes.LastIndexByte,
		bytes.Map,
		bytes.Repeat,
		bytes.Replace,
		bytes.ReplaceAll,
		bytes.Runes,
		bytes.Split,
		bytes.SplitAfter,
		bytes.SplitAfterN,
		bytes.SplitN,
		bytes.Title,
		bytes.ToLower,
		bytes.ToLowerSpecial,
		bytes.ToTitle,
		bytes.ToTitleSpecial,
		bytes.ToUpper,
		bytes.ToUpperSpecial,
		bytes.ToValidUTF8,
		bytes.Trim,
		bytes.TrimFunc,
		bytes.TrimLeft,
		bytes.TrimLeftFunc,
		bytes.TrimPrefix,
		bytes.TrimRight,
		bytes.TrimRightFunc,
		bytes.TrimSpace,
		bytes.TrimSuffix,

		// strconv
		strconv.Atoi,
		strconv.Itoa,
		strconv.ParseBool,
		strconv.ParseFloat,
		strconv.ParseInt,
		strconv.ParseUint,
		strconv.FormatBool,
		strconv.FormatFloat,
		strconv.FormatInt,
		strconv.FormatUint,
		strconv.Quote,
		strconv.QuoteToASCII,
		strconv.QuoteRune,
		strconv.QuoteRuneToASCII,
		strconv.Unquote,
		strconv.UnquoteChar,
		strconv.IsPrint,
		strconv.IsGraphic,

		// math
		math.Abs,
		math.Acos,
		math.Acosh,
		math.Asin,
		math.Asinh,
		math.Atan,
		math.Atan2,
		math.Atanh,
		math.Cbrt,
		math.Ceil,
		math.Copysign,
		math.Cos,
		math.Cosh,
		math.Dim,
		math.Erf,
		math.Erfc,
		math.Erfcinv,
		math.Erfinv,
		math.Exp,
		math.Exp2,
		math.Expm1,
		math.Floor,
		math.Frexp,
		math.Gamma,
		math.Hypot,
		math.Ilogb,
		math.Inf,
		math.IsInf,
		math.J0,
		math.J1,
		math.Jn,
		math.Ldexp,
		math.Lgamma,
		math.Log,
		math.Log10,
		math.Log1p,
		math.Log2,
		math.Logb,
		math.Max,
		math.Min,
		math.Mod,
		math.Modf,
		math.Nextafter,
		math.Pow,
		math.Pow10,
		math.Remainder,
		math.Round,
		math.RoundToEven,
		math.Signbit,
		math.Sin,
		math.Sinh,
		math.Sqrt,
		math.Tan,
		math.Tanh,
		math.Trunc,
		math.Y0,
		math.Y1,
		math.Yn,

		// time
		time.Now,
		time.Date,
		time.Parse,
		time.ParseDuration,
		time.ParseInLocation,
		time.Since,
		time.Until,
		time.Unix,
		time.UnixMilli,
		time.UnixMicro,
		time.Sleep,
		time.LoadLocation,

		// os
		os.Chdir,
		os.Chmod,
		os.Chown,
		os.Clearenv,
		os.Environ,
		os.Executable,
		os.Exit,
		os.Expand,
		os.ExpandEnv,
		os.Hostname,
		os.Lchown,
		os.Link,
		os.LookupEnv,
		os.Mkdir,
		os.MkdirAll,
		os.ReadDir,
		os.ReadFile,
		os.Readlink,
		os.Remove,
		os.RemoveAll,
		os.Rename,
		os.Setenv,
		os.Symlink,
		os.TempDir,
		os.Truncate,
		os.Unsetenv,
		os.UserCacheDir,
		os.UserConfigDir,
		os.UserHomeDir,
		os.WriteFile,

		// filepath
		filepath.Abs,
		filepath.Base,
		filepath.Clean,
		filepath.Dir,
		filepath.EvalSymlinks,
		filepath.Ext,
		filepath.FromSlash,
		filepath.Glob,
		filepath.IsAbs,
		filepath.Join,
		filepath.Match,
		filepath.Rel,
		filepath.Split,
		filepath.SplitList,
		filepath.ToSlash,
		filepath.VolumeName,

		// path
		path.Base,
		path.Clean,
		path.Dir,
		path.Ext,
		path.IsAbs,
		path.Join,
		path.Match,
		path.Split,

		// io
		io.Copy,
		io.CopyN,
		io.ReadAll,
		io.ReadAtLeast,
		io.ReadFull,
		io.WriteString,

		// sort
		sort.Float64s,
		sort.Float64sAreSorted,
		sort.Ints,
		sort.IntsAreSorted,
		sort.Strings,
		sort.StringsAreSorted,

		// regexp
		regexp.Match,
		regexp.MatchString,
		regexp.QuoteMeta,
		regexp.Compile,
		regexp.MustCompile,

		// json
		json.Marshal,
		json.MarshalIndent,
		json.Unmarshal,
		json.Valid,
		json.Compact,
		json.HTMLEscape,

		// hex
		hex.DecodeString,
		hex.DecodedLen,
		hex.Dump,
		hex.EncodeToString,
		hex.EncodedLen,

		// md5
		md5.Sum,
		md5.New,

		// sha256
		sha256.Sum256,
		sha256.New,

		// errors
		errors.New,
		errors.Unwrap,
		errors.Is,
		errors.As,

		// rand
		rand.ExpFloat64,
		rand.Float32,
		rand.Float64,
		rand.Int,
		rand.Int31,
		rand.Int31n,
		rand.Int63,
		rand.Int63n,
		rand.Intn,
		rand.NormFloat64,
		rand.Perm,
		rand.Seed,
		rand.Shuffle,
		rand.Uint32,
		rand.Uint64,

		// url
		url.JoinPath,
		url.Parse,
		url.ParseQuery,
		url.ParseRequestURI,
		url.PathEscape,
		url.PathUnescape,
		url.QueryEscape,
		url.QueryUnescape,

		// utf8
		utf8.DecodeLastRune,
		utf8.DecodeLastRuneInString,
		utf8.DecodeRune,
		utf8.DecodeRuneInString,
		utf8.FullRune,
		utf8.FullRuneInString,
		utf8.RuneCount,
		utf8.RuneCountInString,
		utf8.RuneLen,
		utf8.RuneStart,
		utf8.Valid,
		utf8.ValidRune,
		utf8.ValidString,

		// log
		log.Fatal,
		log.Fatalf,
		log.Fatalln,
		log.Panic,
		log.Panicf,
		log.Panicln,
		log.Print,
		log.Printf,
		log.Println,
		log.SetFlags,
		log.SetPrefix,
	)

	manual := map[string]any{
		// math
		"math.is_nan": math.IsNaN,
		"math.nan":    math.NaN,

		// os getters
		"os.get_egid":     os.Getegid,
		"os.get_env":      os.Getenv,
		"os.get_euid":     os.Geteuid,
		"os.get_gid":      os.Getgid,
		"os.get_groups":   os.Getgroups,
		"os.get_pagesize": os.Getpagesize,
		"os.get_pid":      os.Getpid,
		"os.get_ppid":     os.Getppid,
		"os.get_uid":      os.Getuid,
		"os.get_wd":       os.Getwd,

		// base64
		"base64.std_encoding.decode_string":    base64.StdEncoding.DecodeString,
		"base64.std_encoding.encode_to_string": base64.StdEncoding.EncodeToString,
		"base64.url_encoding.decode_string":    base64.URLEncoding.DecodeString,
		"base64.url_encoding.encode_to_string": base64.URLEncoding.EncodeToString,
	}

	for name, fn := range manual {
		env.Define(name, GoFunc{
			Name: name,
			Func: fn,
		})
	}
}

func register(env *Env, funcs ...any) {
	_ = runtime.Version
	for _, fn := range funcs {
		name := funcName(fn)
		env.Define(name, GoFunc{
			Name: name,
			Func: fn,
		})
	}
}
