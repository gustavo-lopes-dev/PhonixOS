# Acesso Remoto (Tailscale / WireGuard)

Como abrir o dashboard do PhonixOS fora de casa (4G/5G, outra rede Wi-Fi) por uma VPN mesh, sem
root no Android. O servidor continua como está: HTTP na porta `8080`, em todas as interfaces do
aparelho. A VPN só cria um endereço privado (o "IP da VPN") pelo qual seus outros dispositivos
alcançam essa porta.

> **Nunca exponha a porta `8080` à internet** (redirecionamento de porta no roteador, DMZ, túnel
> público). A API do PhonixOS não tem autenticação: ela foi feita para LAN ou VPN privada, nunca
> para acesso público.

## O que funciona por cada endereço

| Endereço usado pelo cliente remoto | Dashboard online (REST + WebSocket) | Instalação PWA e cache offline |
| :--- | :--- | :--- |
| `http://<IP-da-VPN>:8080` (ex.: `100.101.102.103`, `10.8.0.2`) | Sim | Não |
| `http://<nome-MagicDNS>:8080` (Tailscale) | Sim | Não |
| `http://phonix.local:8080` | Não pela VPN: o mDNS é multicast e só funciona dentro da LAN | Não |
| `https://<hostname-real>` via reverse proxy com certificado confiável | Sim | Sim ([HTTPS para a PWA](https_pwa.md)) |

Acesso HTTP por IP de LAN, IP de VPN, nome MagicDNS ou `phonix.local` **não** habilita a PWA em
clientes remotos: o navegador só registra o Service Worker e oferece a instalação em origem segura.
Para isso, veja [PWA por HTTPS através da VPN](#pwa-por-https-através-da-vpn).

## Pré-requisitos

1. PhonixOS instalado e rodando no aparelho (`deploy/quickstart.sh` ou `scripts/install.sh`). No
   Termux: `sv status phonixos` mostra `run:`, e
   `curl -fsS http://127.0.0.1:8080/api/v1/health` responde `"status":"healthy"`.
2. Termux mantido ativo: `termux-wake-lock` e, nas configurações do Android, otimização de bateria
   **sem restrições** para o Termux **e** para o app de VPN. Sem isso, o Android pode suspender a
   VPN ou o servidor com a tela desligada.
3. Os dois apps abaixo usam a API `VpnService` do Android: **não precisam de root**. O Android
   permite **uma VPN ativa por vez**. Tailscale e WireGuard não rodam juntos, nem com outro app que
   use VPN (bloqueadores de anúncio, firewalls sem root, VPNs comerciais).

Escolha **uma** das opções:

- **Tailscale (recomendado):** sem servidor próprio nem porta aberta no roteador, funciona atrás de
  CGNAT de operadora. Depende da coordenação do serviço Tailscale (plano gratuito para uso pessoal).
- **WireGuard puro:** totalmente auto-hospedado, mas exige um *hub* com endereço público e porta UDP
  liberada (servidor em casa com redirecionamento de porta, ou VPS).

## Opção A: Tailscale

### No aparelho do PhonixOS

1. Instale o app **Tailscale** (Google Play ou F-Droid) e faça login.
2. Toque em **Connect** e aceite o pedido de conexão VPN do Android.
3. Anote o IP do aparelho na tailnet (faixa `100.64.0.0/10`, ex.: `100.101.102.103`), mostrado no
   app e no painel de administração (*Machines*).
4. No painel de administração, em *Machines › aparelho › Disable key expiry*, desative a expiração
   de chave desse aparelho. Sem isso ele sai da tailnet quando a chave expira (180 dias por padrão)
   e o dashboard fica inacessível até um novo login no telefone.
5. Recomendado: nas configurações do Android, *Rede e internet › VPN › Tailscale (engrenagem)*,
   ative **VPN sempre ativa**, para o túnel voltar sozinho após reinício ou troca de rede.

### No cliente (notebook, outro celular)

1. Instale o Tailscale e entre **na mesma conta/tailnet**.
2. Abra `http://100.101.102.103:8080` (o IP anotado). Com MagicDNS ativo, também funciona
   `http://<nome-do-aparelho>:8080` ou `http://<nome-do-aparelho>.<sua-tailnet>.ts.net:8080`;
   continua sendo HTTP, com as mesmas limitações de PWA.

Opcional: a política de acesso (ACL) da tailnet pode restringir quem alcança a porta `8080` do
aparelho. A política padrão permite que todos os seus dispositivos se alcancem.

## Opção B: WireGuard (auto-hospedado)

O celular normalmente está atrás de NAT/CGNAT e não recebe conexões de entrada. Por isso a
topologia usa um **hub** alcançável pela internet: o aparelho mantém um túnel de saída até ele
(`PersistentKeepalive`), e o hub encaminha o tráfego dos clientes até o aparelho.

```text
notebook 10.8.0.3 ──┐                         ┌── celular PhonixOS 10.8.0.2
  (4G, outra rede)  └──▶ hub 10.8.0.1 ◀───────┘   (keepalive de saída a cada 25 s)
                    vpn.example.com:51820/udp
```

Endereços de exemplo: rede `10.8.0.0/24`, que **não** pode se sobrepor à faixa da sua LAN nem à da
rede onde o cliente estiver. Troque `vpn.example.com` pelo nome DNS (ou IP público) do hub.

O app oficial **WireGuard** para Android usa a implementação em espaço de usuário (`wireguard-go`)
via `VpnService` quando não há root nem módulo de kernel: nada de especial é necessário.

### Chaves

Cada ponto tem um par de chaves. No app Android, *+ › Criar do zero* gera a chave privada no
próprio aparelho; copie dali a **chave pública**. No hub (e em clientes Linux):

```sh
umask 077
wg genkey | tee hub.key | wg pubkey > hub.pub
```

Chaves privadas nunca saem do dispositivo dono delas e nunca vão para o repositório; só as chaves
públicas são trocadas entre os pontos.

### Hub (Linux) — `/etc/wireguard/wg0.conf`

```ini
[Interface]
Address = 10.8.0.1/24
ListenPort = 51820
PrivateKey = <conteúdo de hub.key>

# Celular com o PhonixOS
[Peer]
PublicKey = <chave pública do celular>
AllowedIPs = 10.8.0.2/32

# Notebook (repita um bloco por cliente, com um IP /32 diferente)
[Peer]
PublicKey = <chave pública do notebook>
AllowedIPs = 10.8.0.3/32
```

O hub precisa encaminhar pacotes entre os pontos do próprio túnel:

```sh
echo 'net.ipv4.ip_forward = 1' | sudo tee /etc/sysctl.d/99-wireguard.conf
sudo sysctl --system
# Somente se a política da cadeia FORWARD for DROP (Docker, firewalls restritivos):
sudo iptables -A FORWARD -i wg0 -o wg0 -j ACCEPT

sudo systemctl enable --now wg-quick@wg0
```

Libere **UDP 51820** no firewall do hub e, se ele estiver em casa, redirecione essa porta UDP no
roteador para o hub. Só o WireGuard fica exposto: a porta `8080` do celular continua acessível
apenas pelo túnel. Se a sua conexão de casa estiver atrás de CGNAT (sem IP público), use uma VPS
como hub ou a opção Tailscale.

### Celular com o PhonixOS — túnel no app WireGuard

```ini
[Interface]
PrivateKey = <gerada pelo app>
Address = 10.8.0.2/32

[Peer]
PublicKey = <conteúdo de hub.pub>
Endpoint = vpn.example.com:51820
AllowedIPs = 10.8.0.0/24
PersistentKeepalive = 25
```

- `AllowedIPs = 10.8.0.0/24` roteia só a rede da VPN (*split tunnel*). Não use `0.0.0.0/0`: o
  servidor não precisa mandar todo o tráfego do celular para casa.
- `PersistentKeepalive = 25` mantém o mapeamento NAT aberto para o hub alcançar o celular. Custa um
  pacote pequeno a cada 25 s; sem ele o dashboard só responde depois que o celular enviar algo.
- Ative o túnel e, como no Tailscale, **VPN sempre ativa** nas configurações do Android.

### Cliente remoto (notebook)

```ini
[Interface]
PrivateKey = <conteúdo da chave privada do notebook>
Address = 10.8.0.3/32

[Peer]
PublicKey = <conteúdo de hub.pub>
Endpoint = vpn.example.com:51820
AllowedIPs = 10.8.0.0/24
```

Linux: `sudo wg-quick up ./notebook.conf` (ou em `/etc/wireguard/`). Windows/macOS/iOS/Android:
importe o arquivo no app WireGuard. Depois abra `http://10.8.0.2:8080`.

No hub, `sudo wg show` deve listar um *latest handshake* recente para o celular e para o cliente.

## Verificação

De um cliente **fora** da rede de casa (ex.: notebook no roteador do celular de outra pessoa ou
em 4G), com a VPN ativa e `V` apontando para o IP da VPN do aparelho:

```sh
V=http://100.101.102.103:8080        # Tailscale; no WireGuard: http://10.8.0.2:8080

# 1. API pelo túnel
curl -fsS "$V/api/v1/health"         # {"success":true,"data":{"status":"healthy",...}}

# 2. SPA incorporada no binário
curl -fsS "$V/" | grep -q '/_app/immutable/' && echo spa-ok

# 3. WebSocket de telemetria (espera 101)
curl -sS -o /dev/null -w '%{http_code}\n' --http1.1 --max-time 3 \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' "$V/ws/telemetry"
```

O `curl` do passo 3 encerra por timeout depois de receber o `101`; o código impresso é o que
importa. No navegador, `http://<IP-da-VPN>:8080/` deve abrir o dashboard com o indicador de conexão
em "ao vivo".

O servidor registra no boot as URLs `http://<IPv4>:8080` das interfaces ativas naquele momento. Se
a VPN subiu antes do PhonixOS, o IP dela pode aparecer nessa lista; se subiu depois, não aparece.
Em ambos os casos, o endereço de referência é o mostrado pelo app de VPN.

## Solução de problemas

| Sintoma | Causa provável e correção |
| :--- | :--- |
| Timeout no IP da VPN | VPN desligada em uma das pontas, ou outro app de VPN ativo no celular (só uma por vez). No WireGuard, confira o *handshake* com `wg show` no hub. |
| Handshake ok, mas sem resposta (WireGuard) | Encaminhamento desligado no hub (`ip_forward`, cadeia `FORWARD`), ou `AllowedIPs` do celular sem a rede do cliente (`10.8.0.0/24`). |
| Funciona com a tela ligada e cai depois | Otimização de bateria do Android: deixe Termux e o app de VPN **sem restrições**, use `termux-wake-lock` e **VPN sempre ativa**. |
| `phonix.local` não resolve fora de casa | Esperado: mDNS não atravessa a VPN. Use o IP da VPN ou o nome MagicDNS. |
| API responde, mas o dashboard não instala como app | Esperado em HTTP. Veja a seção abaixo. |
| Faixa `10.8.0.0/24` já usada na rede local | Troque a sub-rede em todos os arquivos (hub, celular e clientes) por outra livre, ex.: `10.66.0.0/24`. |

## PWA por HTTPS através da VPN

A VPN resolve a **alcançabilidade**; ela não transforma `http://<IP-da-VPN>:8080` em origem segura.
Para instalar a PWA e usar o cache offline num cliente remoto, siga
[HTTPS para a PWA na LAN](https_pwa.md), aplicado à VPN:

- O reverse proxy roda num host **dentro da VPN** (ex.: o hub WireGuard, ou um PC na tailnet) e
  aponta para `http://<IP-da-VPN-do-celular>:8080`.
- O certificado precisa ser confiável e válido para o hostname digitado no navegador, cujo DNS
  aponta para o IP **do proxy na VPN** (desafio ACME DNS-01, como no guia). Na Tailscale, um host
  com a CLI pode obter certificados para o seu nome `*.ts.net` (`tailscale cert`, após habilitar
  HTTPS no painel); esses nomes ficam registrados nos logs públicos de Certificate Transparency.
- SPA (`/`), API (`/api/v1/`) e WebSocket (`/ws/telemetry`, com upgrade) passam pela **mesma
  origem** no proxy; não aponte o navegador direto para a porta `8080`.
- Nunca aceite aviso de certificado nem use flags de origem insegura do navegador como substituto.
