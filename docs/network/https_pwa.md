# HTTPS para a PWA na LAN

O servidor PhonixOS roda sem root no Termux, em HTTP na porta `8080`. Isso basta para usar o
dashboard **online** de qualquer cliente da rede. Instalar a PWA e usar o cache offline exige outra
coisa: uma **origem segura**, ou seja, HTTPS com certificado confiável para o hostname acessado.

## O que cada origem oferece

| Origem acessada pelo cliente | Dashboard online (REST + WebSocket) | Service Worker, cache offline, instalação |
| :--- | :--- | :--- |
| `http://<ip-da-LAN>:8080` | Sim | Não |
| `http://phonix.local:8080` | Sim | Não |
| `http://localhost:8080` (no próprio aparelho) | Sim | Sim (o navegador trata `localhost` como seguro) |
| `https://<hostname-real>` via reverse proxy com certificado confiável | Sim | Sim |

Endereços HTTP por IP ou `.local` **não são origens seguras**: o navegador não registra o Service
Worker nem oferece a instalação. Não contorne isso desativando proteções do navegador
(flags de "insecure origins", exceções de certificado, aceitar certificado inválido). Use o
reverse proxy abaixo.

## Arquitetura

```text
cliente (navegador) ──HTTPS 443──▶ reverse proxy (hostname real, certificado confiável)
                                        │  mesma origem: /  /api/v1/  /ws/ (upgrade)
                                        └──HTTP──▶ Termux  http://<ip-do-aparelho>:8080
```

- O proxy fica num host alcançável pelos clientes: PC, servidor caseiro, roteador, ou o próprio
  aparelho se o proxy puder rodar sem root em porta alta (ex.: `8443`). O servidor PhonixOS continua
  sem root, ouvindo `8080`.
- Tudo sai pela **mesma origem**: a SPA (`/`), a API (`/api/v1/`) e o WebSocket (`/ws/telemetry`).
  O frontend monta as URLs a partir de `location`, então em HTTPS usa `wss://` automaticamente. Não
  separe a API em outro host ou porta.
- Reserve um IP fixo (lease DHCP) para o aparelho, pois o proxy aponta para ele.

## Certificado confiável para um hostname real

O certificado precisa ser válido para o nome digitado no navegador (ex.: `phonix.casa.example.com`).
`.local` e IPs privados não recebem certificado de CA pública. Opções:

1. **Domínio próprio + ACME com desafio DNS-01 (recomendado).** Crie um registro `A` de
   `phonix.casa.example.com` apontando para o IP **do proxy** na LAN (no DNS público ou num DNS local
   que os clientes usem). O desafio DNS-01 emite o certificado sem expor nada à internet. Alguns
   roteadores bloqueiam respostas DNS públicas com IP privado (proteção contra *DNS rebinding*);
   nesse caso, use o DNS local do roteador/Pi-hole para o registro, ou libere o domínio nessa proteção.
2. **CA interna instalada nos clientes.** Válida quando você administra todos os dispositivos: a CA
   raiz precisa ser instalada no repositório de confiança de cada cliente. Sem isso, o navegador
   rejeita o certificado, e aceitar o aviso manualmente não torna a origem segura.

## Exemplo: Caddy

Caddy repassa o upgrade de WebSocket sem configuração extra. Com DNS-01 é preciso uma build com o
módulo do seu provedor DNS (ex.: `caddy-dns/cloudflare`); o token vai em variável de ambiente do
host do proxy, nunca no repositório.

```caddyfile
phonix.casa.example.com {
	tls {
		dns cloudflare {env.CF_API_TOKEN}
	}
	reverse_proxy 192.168.1.150:8080
}
```

## Exemplo: nginx

Certificado obtido por qualquer cliente ACME com DNS-01 (ex.: `certbot` com plugin do provedor).

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl;
    server_name phonix.casa.example.com;

    ssl_certificate     /etc/letsencrypt/live/phonix.casa.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/phonix.casa.example.com/privkey.pem;

    # SPA e API REST
    location / {
        proxy_pass http://192.168.1.150:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Telemetria em tempo real: upgrade HTTP/1.1 e timeout longo (aba em segundo plano pausa o stream)
    location /ws/ {
        proxy_pass http://192.168.1.150:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Host $host;
        proxy_read_timeout 1h;
        proxy_send_timeout 1h;
    }
}
```

O proxy não deve cachear nem reescrever `Cache-Control`: o servidor marca `/_app/immutable/` como
imutável e o restante (HTML, `service-worker.js`, `/_app/version.json`, manifesto) como `no-cache`,
e `/api/v1/` nunca deve ser cacheado.

## Verificação

Do host de um cliente da LAN, com `H=https://phonix.casa.example.com`:

```sh
# 1. Certificado aceito sem -k (cadeia confiável para o hostname)
curl -fsS "$H/api/v1/health"                     # {"success":true,"data":{"status":"healthy",...}}

# 2. SPA e bundle pela mesma origem
curl -fsS "$H/" | grep -q '/_app/immutable/' && echo spa-ok
curl -fsSI "$H/manifest.webmanifest" | grep -i '^content-type: application/manifest+json'
curl -fsSI "$H/service-worker.js" | grep -i '^cache-control: no-cache'

# 3. Upgrade WebSocket repassado (espera 101)
curl -sS -o /dev/null -w '%{http_code}\n' --http1.1 --max-time 3 \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' "$H/ws/telemetry"
```

O `curl` do passo 3 encerra por timeout depois de receber o `101`; o código impresso é o que importa.

No navegador do cliente (Chrome/Edge: DevTools › Application):

1. Abrir `https://phonix.casa.example.com/` sem aviso de certificado (cadeado normal).
2. **Manifest** sem erros de instalabilidade; **Service Workers** com `service-worker.js` ativo.
3. O indicador de conexão do dashboard fica em "ao vivo" (WebSocket `wss://` aberto em **Network › WS**).
4. A opção de instalar aparece (menu do navegador ou aviso de instalação do PhonixOS).
5. Com o app instalado e o servidor parado, a casca abre do cache e o dashboard mostra conexão
   perdida em vez de erro do navegador.

## Fallback HTTP

Sem proxy, `http://<ip-da-LAN>:8080` (e `http://phonix.local:8080`, quando o mDNS estiver ativo)
continua sendo o acesso padrão: dashboard completo e telemetria em tempo real, porém sem instalação
nem uso offline. O servidor imprime esses endereços no boot.
