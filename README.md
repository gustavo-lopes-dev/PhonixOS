# PhonixOS

<img width="484" height="516" alt="c9031bc2-b4d9-4446-bd07-b8e940f9d32e-removebg-preview" src="https://github.com/user-attachments/assets/7058e5b1-171c-4e21-8ef2-cfe6903e01b1" />

Painel de monitoramento para Android que roda no Termux, sem root: um servidor Go único
(porta `8080`) com a interface web incorporada, supervisionado pelo `termux-services`.

## Requisitos no aparelho

- **Termux** (F-Droid ou GitHub). A instalação (`scripts/install.sh`) instala sozinha os
  pacotes que faltarem (`curl`, `coreutils`, `termux-services`) e registra os serviços.
- **Termux:API — necessário para as informações de bateria.** O Android não permite que apps
  comuns leiam a bateria diretamente (`/sys/class/power_supply` e `dumpsys` são bloqueados). Sem o
  Termux:API, nível, status, saúde, temperatura e conexão do carregador aparecem como **N/D**.
  1. Instale o app **Termux:API** da **mesma fonte** do seu Termux (F-Droid com F-Droid, GitHub
     com GitHub; fontes diferentes não se comunicam por usarem assinaturas distintas).
  2. No Termux: `pkg install termux-api`.
  3. Confira com `termux-battery-status`, que deve imprimir um JSON com `percentage`. O PhonixOS
     passa a usar essa fonte automaticamente, sem reinstalar nem reiniciar.

## Fontes de dados e compatibilidade (sem root)

Cada informação percorre fontes em ordem; se uma for bloqueada pelo Android, a próxima é usada.

| Informação | Fonte usada quando o Android bloqueia a padrão |
| :--- | :--- |
| Uso de CPU e por núcleo | `/proc/stat` → tempo ocioso em `/sys/devices/system/cpu/*/cpuidle` |
| Load average | `/proc/loadavg` → syscall `sysinfo` |
| IP da rede | netlink → `ioctl` (`SIOCGIFCONF`) |
| Taxas de Up/Down | `/proc/net/dev` → `/sys/class/net/*/statistics` → helper Java (`app_process`): `netstats` por interface (Android 13+ e 10–12) → `TrafficStats` por interface (7–12) → total do aparelho (`netstats`/`TrafficStats`), usado só quando há uma única interface ativa → sem fonte: IP exibido e taxas N/D |
| Bateria | sysfs → `dumpsys` → Termux:API (veja acima) → N/D |

O helper Java só roda enquanto o dashboard está aberto (~50 MB de RAM nesse período; encerrado
20 s após a última leitura). A fonte em uso de cada informação aparece no log do serviço
(`collector: telemetry source selected`).

## Acesso

Ao final, o instalador imprime os endereços `http://<ip-do-aparelho>:8080`. Em outros
dispositivos da rede, o dashboard via HTTP funciona online; instalação como app (PWA) e cache
offline exigem HTTPS confiável: veja [`docs/network/https_pwa.md`](docs/network/https_pwa.md).
Build e publicação: [`docs/release.md`](docs/release.md).
