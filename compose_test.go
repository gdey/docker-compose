package dockercompose

import (
	"testing"
)

func tcurry[TC any](fn func(*testing.T,TC)) func(TC)func(*testing.T){
	return func(tc TC)func(*testing.T){ return func (t *testing.T){ fn(t,tc) }}
}

func Test_StartService(t *testing.T) {
	type tcase struct {
		Service string
		repeat  uint   // number of additional calls to start with service name
		count   uint32 // expected additional count afterwards
	}
	fn := tcurry(func(t *testing.T, tc tcase) {
		initialStatuses, err := PS()
		if err != nil {

			panic(err)
		}
		updateProcessStatuses(initialStatuses)
		baseServiceStatus := getRunningServiceStatus(tc.Service)
		t.Logf("base status: %v", baseServiceStatus)
		if _, err = Start(tc.Service); err != nil {
			t.Errorf("errored, expected nil, got %v", err)
		}
		for i := uint(0); i < tc.repeat; i++ {
			if _, err = Start(tc.Service); err != nil {
				t.Errorf("repeated(%v) errored, expected nil, got %v", i, err)
			}
		}
		afterServiceStatus := getRunningServiceStatus(tc.Service)
		t.Logf("after status: %v", afterServiceStatus)
		if afterServiceStatus.count != baseServiceStatus.count+tc.count {
			t.Errorf("%v count, expected %v got %v", tc.Service, baseServiceStatus.count+tc.count, afterServiceStatus.count)
		}
		if err = Stop(tc.Service); err != nil {
			t.Errorf("stop errored, expected nil, got %v", err)
		}
		for i := uint(0); i < tc.repeat; i++ {
			if err = Stop(tc.Service); err != nil {
				t.Errorf("stop repeated(%v) errored, expected nil, got %v", i, err)
			}
		}
		afterServiceStatus = getRunningServiceStatus(tc.Service)
		t.Logf("after stop status: %v", afterServiceStatus)
		if afterServiceStatus.count != baseServiceStatus.count {
			t.Errorf("%v count, expected %v got %v", tc.Service, baseServiceStatus.count, afterServiceStatus.count)
		}

	})
	tests := map[string]tcase{
		"up once minio": {
			Service: "minio",
			count:   1,
		},
		"up twice minio": {
			Service: "minio",
			repeat:  1,
			count:   2,
		},
	}
	for name, tc := range tests {
		t.Run(name, fn(tc))
	}
}
