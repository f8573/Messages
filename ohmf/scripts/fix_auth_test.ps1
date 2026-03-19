$path = 'C:\Users\James\Downloads\Messages\ohmf\services\gateway\internal\auth\auth_test.go'
$code = Get-Content -Raw $path
$code = $code -replace 'auth\.NewService', 'auth.NewHandler'
$code = $code -replace '\*auth\.Service', '*auth.Handler'
$code = $code -replace 'svc\.StartPhoneVerification', 'svc.StartPhoneVerification'
Set-Content -Path $path -Value $code
