{ pkgs, lib, ... }:
let
  tls-cert = pkgs.runCommand "selfSignedCerts" { buildInputs = [ pkgs.openssl ]; } ''
    openssl req \
      -x509 -newkey rsa:4096 -sha256 -days 365 \
      -nodes -out cert.pem -keyout key.pem \
      -subj '/CN=slopscale' -addext "subjectAltName=DNS:slopscale"

    mkdir -p $out
    cp key.pem cert.pem $out
  '';
in
{
  name = "slopscale";
  meta.maintainers = [
    {
      name = "Cong Tran";
      email = "trancong12102@gmail.com";
      github = "trancong12102";
    }
  ];

  nodes =
    let
      slopscalePort = 8080;
      stunPort = 3478;
      peer = {
        services.tailscale.enable = true;
        security.pki.certificateFiles = [ "${tls-cert}/cert.pem" ];
      };
    in
    {
      peer1 = peer;
      peer2 = peer;

      slopscale = {
        # The module under test is this repository's, not the headscale one in nixpkgs.
        imports = [ ../module.nix ];
        services = {
          slopscale = {
            enable = true;
            port = slopscalePort;
            settings = {
              server_url = "https://slopscale";
              ip_prefixes = [ "100.64.0.0/10" ];
              derp = {
                server = {
                  enabled = true;
                  region_id = 999;
                  stun_listen_addr = "0.0.0.0:${toString stunPort}";
                };
                urls = [ ];
              };
              dns = {
                base_domain = "tailnet";
                extra_records = [
                  {
                    name = "foo.bar";
                    type = "A";
                    value = "100.64.0.2";
                  }
                ];
                override_local_dns = false;
              };
            };
          };
          nginx = {
            enable = true;
            virtualHosts.slopscale = {
              addSSL = true;
              sslCertificate = "${tls-cert}/cert.pem";
              sslCertificateKey = "${tls-cert}/key.pem";
              locations."/" = {
                proxyPass = "http://127.0.0.1:${toString slopscalePort}";
                proxyWebsockets = true;
              };
            };
          };
        };
        networking.firewall = {
          allowedTCPPorts = [
            80
            443
          ];
          allowedUDPPorts = [ stunPort ];
        };
        environment.systemPackages = [ pkgs.slopscale ];
      };
    };

  testScript = ''
    start_all()
    slopscale.wait_for_unit("slopscale")
    slopscale.wait_for_open_port(443)

    # Create slopscale user and preauth-key
    slopscale.succeed("slopscale users create test")
    authkey = slopscale.succeed("slopscale preauthkeys -u 1 create --reusable")

    # Connect peers
    up_cmd = f"tailscale up --login-server 'https://slopscale' --auth-key {authkey}"
    peer1.execute(up_cmd)
    peer2.execute(up_cmd)

    # Check that they are reachable from the tailnet
    peer1.wait_until_succeeds("tailscale ping peer2")
    peer2.wait_until_succeeds("tailscale ping peer1.tailnet")
    assert (res := peer1.wait_until_succeeds("${lib.getExe pkgs.dig} +short foo.bar").strip()) == "100.64.0.2", f"Domain {res} did not match 100.64.0.2"
  '';
}
