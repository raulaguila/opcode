# Cofre nativo opcional

Em **Conexões → Editar → Autenticação**, escolha **Cofre do sistema** para guardar
uma chave no cofre do sistema operacional. Cole a chave no campo mascarado e
salve explicitamente. Chave direta no JSON, variável de ambiente e conexão sem
chave continuam disponíveis; não há migração automática nem fallback entre eles.

O JSON guarda somente `apiKeyRef`, por exemplo:

```json
{
  "providerProfiles": {
    "office": {
      "name": "Office",
      "backend": "openai",
      "baseURL": "https://gateway.example/v1",
      "apiKeyRef": "keyring:provider-ABCDEFGHIJKLMNOPQRST"
    }
  }
}
```

A referência é gerada ao salvar; copiar esse exemplo não cria uma chave. Não
combine `apiKeyRef` com chave literal ou variável de ambiente. A mesma opção é
compatível com conexões legadas em `providers`. O serviço do cofre é
`opcode.providers`, e cada gravação/rotação usa uma conta aleatória nova.

A implementação usa [go-keyring v0.2.8](https://github.com/zalando/go-keyring):
Secret Service por D-Bus no Linux, Keychain no macOS e Credential Manager no
Windows. O sistema precisa disponibilizar e desbloquear seu cofre. A integração
macOS envia o segredo por stdin ao utilitário do sistema, sem shell ou segredo
nos argumentos. Somente chaves do cofre têm limite de **2048 bytes** para respeitar
os limites dos backends; chaves maiores podem usar as opções explícitas existentes.

Abrir a lista/configuração, inventariar linguagens e usar `config show`, `config
validate` ou `doctor` offline não lê o cofre. Consultar catálogo, testar uma
conexão ou usar seu modelo pode ler a chave; o startup online pode consultar o
modelo selecionado para atualizar metadados. Os valores resolvidos permanecem
em memória e participam da redação de credenciais, sem entrar no JSON exportado.
Editar uma entrada existente mostra o campo vazio: deixe vazio para manter sua
chave, ou cole outra para substituir. Uma chave resolvida nunca é copiada para o
modo de chave direta ao mudar a autenticação.

O desbloqueio nativo não oferece cancelamento na biblioteca. Durante uma gravação
a interface informa que está aguardando o SO, bloqueia novas gravações e aguarda
o resultado antes de sair do formulário. A limpeza nativa após falha também
ocorre fora do loop da interface e mantém esse estado até terminar. Não existe timeout que reporte falha e
grave a configuração depois. Consultar catálogo não salva o candidato.

A gravação cria a nova entrada antes de persistir a referência atomicamente. Se
validação ou persistência falhar, a configuração anterior permanece e a entrada
nova é removida; uma falha de limpeza é informada sem mostrar saída nativa. A
configuração nunca recebe a chave em texto como recuperação de falha do cofre.
Se o processo for encerrado abruptamente entre essas etapas, pode restar uma
entrada sem referência; não é uma transação distribuída entre filesystem e SO.

Entradas antigas são mantidas ao trocar chave, autenticação ou excluir conexão,
pois a referência pode estar em outra camada/perfil ou backup. Remova entradas
sem uso pelo gerenciador de credenciais do SO depois de conferir suas referências.
Isso não apaga chaves literais de backups já existentes. Não há exclusão global
ou acesso a outros serviços do cofre.

Testes usam um backend injetado: comprovam referência, limites, recusa, redação,
falta de fallback, rollback e preservação de rascunho sem acessar o cofre real.
O CI não solicita desbloqueio nem comprova integração com o cofre de um usuário.
