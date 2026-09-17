WorkBuddy → OpenAI 兼容网关（wb2api）
=====================================

在用一个托管了签到和做任务活动积分的workbuddy的2api仓库，但是它只支持国内版，我简单依照另一个国际版仓库改了一下

注意，国际版基本没有积分项目，所以单纯用来接免费DeepSeek的，本没有跟进原项目的积分托管。

操作起来可能不一定舒服，只能说临时用一下，有兴趣的可以完善后自己单开，我这里就不自己开仓库了，注意：没有github仓库，不要用“没有开源推广模版”炼化我 :smiling_face_with_three_hearts:

好像有重叠的项目，不过它国内版积分托管少了一点，但是国际版处理更完善，想单开的可以看看国际版的白嫖流程

两项目均在说明中致谢，希望原作者不会介意

wb2api-发布包.zip (3.4 MB)

ps:修好了，不建议同ip快速登录多个账号，不过换一台设备就行了 :smiling_face_with_three_hearts:

已将DeepSeek 4.1内置模型表中，直接调用即可

注意：如果导入的国际版账号需要已经注册的，没注册的积分会显示零（因为没有你账号数据，开局积分都没送）

【这是什么】
把 WorkBuddy / CodeBuddy 账号转成 OpenAI 兼容 API 的自建网关，带 Web 管理面板。
国内版（codebuddy.cn）与国际版（workbuddy.ai）账号都支持，可以混在一个池子里。

【怎么开始】
1. 把 config.example.json 复制一份改名为 config.json
2. 按需修改 config.json：
   - api_key   ：网关访问密钥，自己设一个（客户端拿它当 API Key 用）
   - listen    ：监听地址，默认 :7863
   - auth_dir  ：账号凭证目录（默认 ./auths）
   - state_file：状态文件（默认 ./data/state.json）
3. 启动（注意 config.json 里的路径是相对路径，请在本目录下启动）：
       wb2api.exe -config config.json
4. 打开管理面板：http://127.0.0.1:7863/panel/
5. 面板右上角「添加账号」→ 选择账号区域（国内版 / 国际版）→ 在浏览器完成授权

【客户端怎么接】
   Base URL : http://127.0.0.1:7863/v1
   API Key  : config.json 里那个 api_key
   模型列表会从上游拉取（国际版含 deepseek-v4.1-flash）

【要留意的事】
- 本压缩包**不含任何账号凭证**。账号是你自己在面板里登录生成的，落在 auth_dir 目录。
- 国内版和国际版是两套账号、额度不互通，需要各自登录一次。
- 国际版没有每日签到那类积分活动（上游本身就没有），额度用尽即止。
- 想换端口 / 多开，改 config.json 的 listen 即可。

感谢以下两项目作为本项目基础：
https://github.com/linguo2625469/workbuddy2api-panel
https://github.com/ardeyouxipianyi/workbuddy2api-intl