module lab_env

go 1.22

replace lab_env/service => ./service

require lab_env/service v0.0.0-00010101000000-000000000000

require gopkg.in/yaml.v3 v3.0.1 // indirect
