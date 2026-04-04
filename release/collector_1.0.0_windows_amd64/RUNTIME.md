# У԰��Դ�ɼ�ϵͳ - ����˵��

## �״����в���

1. ���� JWT ��Կ�ԣ����״�������Ҫ��:
- Windows: ֱ�Ӱ� double-click generate-keys.bat
- Linux: ���� ./generate-keys.sh
- ���ֶ�ִ�У�.\\keygen.exe -o keys -s 2048

2. ����Ӧ�ó���:
- ���� config.yaml.example Ϊ config.yaml
- �޸������ļ��е����ݿ�·�����˿ڵ�����

3. ���г���:
- Windows: .\collector.exe
- Linux: ./collector

## Ŀ¼�ṹ
- bin/ - �����Ŀ�ִ���ļ�
- config.yaml - �����ļ�
- data/ - ���ݿ��ļ�Ŀ¼
- keys/ - JWT ��Կ�ļ�Ŀ¼
- logs/ - ��־�ļ�Ŀ¼
- runtime/ - �ⲿ���ع��� (yt-dlp, lux)
- downloads/ - ��������ļ�Ŀ�?

## ע������
1. ˽Կ�ļ� (keys/private.pem) �����Ʊ��ܣ���Ҫй¶
2. ��Ҫ��˽Կ�ύ���汾����ϵͳ
3. ������������ʹ�� 4096 λ��Կ
