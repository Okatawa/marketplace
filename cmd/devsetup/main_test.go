package main
import("os";"path/filepath";"testing")
func TestDevSetupPreservesSecrets(t *testing.T){ d:=t.TempDir();if err:=setup(d);err!=nil{t.Fatal(err)};a,_:=os.ReadFile(filepath.Join(d,".env"));if err:=setup(d);err!=nil{t.Fatal(err)};b,_:=os.ReadFile(filepath.Join(d,".env"));if string(a)!=string(b){t.Fatal("setup rotated existing secrets")};info,_:=os.Stat(filepath.Join(d,".env"));if info.Mode().Perm()!=0600{t.Fatal("secrets readable by others")}}
