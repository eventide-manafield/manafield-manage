package sso

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "net/url"
 "strings"
 "testing"
)
func TestUnauthenticatedRequestsRequireLogin(t *testing.T) {
 g:=New(Config{AuthorizationURL:"https://account.test/account/oauth/authorize",TokenURL:"http://unused/token",UserInfoURL:"http://unused/userinfo",ClientID:"manage",RedirectURL:"https://manage.test/auth/callback"},nil)
 next:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(200);w.Write([]byte("PRIVATE_CONTENT"))})
 h:=g.Wrap(next)
 for _,path:=range []string{"/","/modules/example","/static/app.css","/static/custom.css"} {
  w:=httptest.NewRecorder();h.ServeHTTP(w,httptest.NewRequest("GET","https://manage.test"+path,nil))
  if w.Code!=303||w.Header().Get("Location")!="/auth/login"||strings.Contains(w.Body.String(),"PRIVATE_CONTENT"){t.Fatalf("path %q leaked: %d %q",path,w.Code,w.Body.String())}
 }
 w:=httptest.NewRecorder();h.ServeHTTP(w,httptest.NewRequest("POST","https://manage.test/modules/example",nil))
 if w.Code!=401{t.Fatalf("anonymous write %d",w.Code)}
 w=httptest.NewRecorder();h.ServeHTTP(w,httptest.NewRequest("GET","https://manage.test/manafield/health",nil))
 if w.Code!=200{t.Fatalf("health %d",w.Code)}
}
func TestPKCELoginCallbackAndRevocation(t *testing.T) {
 accessToken:=strings.Repeat("t",43)
 active:=true
 issuer:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path {
  case "/token":
   r.ParseForm()
   if r.Method!="POST"||len(r.PostForm.Get("code_verifier"))!=43||r.PostForm.Get("code")!=strings.Repeat("c",43) {t.Errorf("invalid token exchange");http.Error(w,"invalid",400);return}
   json.NewEncoder(w).Encode(map[string]any{"access_token":accessToken,"token_type":"Bearer","expires_in":900})
  case "/userinfo":
   if !active {http.Error(w,"invalid",401);return}
   if r.Header.Get("Authorization")!="Bearer "+accessToken{http.Error(w,"invalid",401);return}
   json.NewEncoder(w).Encode(map[string]string{"sub":"00000000-0000-4000-8000-000000000001"})
  default:http.NotFound(w,r)
  }
 }))
 defer issuer.Close()
 g:=New(Config{AuthorizationURL:"https://account.test/account/oauth/authorize",TokenURL:issuer.URL+"/token",UserInfoURL:issuer.URL+"/userinfo",ClientID:"manage",RedirectURL:"https://manage.test/auth/callback"},issuer.Client())
 h:=g.Wrap(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Write([]byte("PRIVATE_CONTENT"))}))
 w:=httptest.NewRecorder();h.ServeHTTP(w,httptest.NewRequest("GET","https://manage.test/auth/login",nil))
 if w.Code!=303{t.Fatalf("login %d",w.Code)}
 u,err:=url.Parse(w.Header().Get("Location"));if err!=nil{t.Fatal(err)}
 if u.Query().Get("code_challenge_method")!="S256"||len(u.Query().Get("code_challenge"))!=43{t.Fatal("no PKCE S256")}
 cookie:=w.Result().Cookies()[0]
 state:=u.Query().Get("state")
 callback:="https://manage.test/auth/callback?state="+url.QueryEscape(state)+"&code="+strings.Repeat("c",43)
 req:=httptest.NewRequest("GET",callback,nil);req.AddCookie(cookie)
 done:=httptest.NewRecorder();h.ServeHTTP(done,req)
 if done.Code!=303||done.Header().Get("Location")!="/"{t.Fatalf("callback %d %q",done.Code,done.Body.String())}
 var sessionCookie *http.Cookie
 for _,c:=range done.Result().Cookies(){if c.Name==sessionCookie{sessionCookie=c}}
 if sessionCookie==nil||!sessionCookie.HttpOnly||!sessionCookie.Secure{t.Fatal("insecure session cookie")}
 private:=httptest.NewRequest("GET","https://manage.test/",nil);private.AddCookie(sessionCookie)
 got:=httptest.NewRecorder();h.ServeHTTP(got,private)
 if got.Code!=200||!strings.Contains(got.Body.String(),"PRIVATE_CONTENT"){t.Fatalf("logged in %d",got.Code)}
 active=false
 denied:=httptest.NewRecorder();h.ServeHTTP(denied,private)
 if denied.Code!=303{t.Fatalf("revoked access %d",denied.Code)}
}
func TestForgedCallbackAndLogoutCSRF(t *testing.T) {
 g:=New(Config{AuthorizationURL:"https://account.test/account/oauth/authorize",ClientID:"manage",RedirectURL:"https://manage.test/auth/callback"},nil)
 h:=g.Wrap(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(200)}))
 bad:=httptest.NewRecorder();h.ServeHTTP(bad,httptest.NewRequest("GET","https://manage.test/auth/callback?state=x&code=y",nil))
 if bad.Code!=400{t.Fatalf("forged callback %d",bad.Code)}
 req:=httptest.NewRequest("POST","https://manage.test/auth/logout",nil)
 req.Header.Set("Origin","https://evil.test")
 denied:=httptest.NewRecorder();h.ServeHTTP(denied,req)
 if denied.Code!=403{t.Fatalf("logout CSRF %d",denied.Code)}
}
