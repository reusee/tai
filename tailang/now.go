package tailang

import "time"

type Now struct {
	Format string `tai:"format"`
	In     string `tai:"in"`
}

var _ Function = Now{}

func (n Now) FunctionName() string {
	return "now"
}

func (n Now) Call() (string, error) {
	t := time.Now()

	if n.In != "" {
		location, err := time.LoadLocation(n.In)
		if err != nil {
			return "", err
		}
		t = t.In(location)
	}

	if n.Format != "" {
		return t.Format(n.Format), nil
	}

	return t.String(), nil
}
