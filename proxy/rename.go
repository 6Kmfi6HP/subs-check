package proxy

import (
	"regexp"
	"strconv"
	"sync/atomic"
)

// CountryInfo 存储国家的信息
type CountryInfo struct {
	// 国家的emoji图标
	Emoji string
	// 国家的中文名称
	Name string
	// 匹配模式 - 用于识别国家
	Pattern string
	// 计数器 - 用于生成唯一的节点名称
	Counter *int32
}

// Counter 用于存储各个地区的计数
type Counter struct {
	// 东亚
	hk int32 // 香港
	tw int32 // 台湾
	jp int32 // 日本
	kr int32 // 韩国

	// 东南亚
	sg int32 // 新加坡
	my int32 // 马来西亚
	th int32 // 泰国
	vn int32 // 越南
	ph int32 // 菲律宾
	id int32 // 印度尼西亚
	kh int32 // 柬埔寨
	mm int32 // 缅甸
	bn int32 // 文莱
	la int32 // 老挝

	// 南亚和中亚
	in int32 // 印度
	pk int32 // 巴基斯坦
	bd int32 // 孟加拉国
	np int32 // 尼泊尔
	lk int32 // 斯里兰卡
	kz int32 // 哈萨克斯坦
	uz int32 // 乌兹别克斯坦

	// 中东
	ae int32 // 阿联酋
	ir int32 // 伊朗
	sa int32 // 沙特阿拉伯
	il int32 // 以色列
	tr int32 // 土耳其
	qa int32 // 卡塔尔
	kw int32 // 科威特
	om int32 // 阿曼
	jo int32 // 约旦
	lb int32 // 黎巴嫩

	// 欧洲
	uk int32 // 英国
	de int32 // 德国
	fr int32 // 法国
	it int32 // 意大利
	es int32 // 西班牙
	pt int32 // 葡萄牙
	nl int32 // 荷兰
	be int32 // 比利时
	ch int32 // 瑞士
	at int32 // 奥地利
	se int32 // 瑞典
	no int32 // 挪威
	dk int32 // 丹麦
	fi int32 // 芬兰
	ie int32 // 爱尔兰
	pl int32 // 波兰
	ru int32 // 俄罗斯
	ua int32 // 乌克兰
	hu int32 // 匈牙利
	cz int32 // 捷克
	gr int32 // 希腊
	ro int32 // 罗马尼亚
	bg int32 // 保加利亚
	hr int32 // 克罗地亚
	rs int32 // 塞尔维亚
	sk int32 // 斯洛伐克
	si int32 // 斯洛文尼亚
	ee int32 // 爱沙尼亚
	lv int32 // 拉脱维亚
	lt int32 // 立陶宛
	is int32 // 冰岛
	lu int32 // 卢森堡
	mc int32 // 摩纳哥

	// 北美洲
	us int32 // 美国
	ca int32 // 加拿大
	mx int32 // 墨西哥

	// 中美洲和加勒比
	pa int32 // 巴拿马
	cr int32 // 哥斯达黎加
	cu int32 // 古巴
	do int32 // 多米尼加

	// 南美洲
	br int32 // 巴西
	ar int32 // 阿根廷
	cl int32 // 智利
	co int32 // 哥伦比亚
	pe int32 // 秘鲁
	ve int32 // 委内瑞拉
	uy int32 // 乌拉圭
	ec int32 // 厄瓜多尔

	// 大洋洲
	au int32 // 澳大利亚
	nz int32 // 新西兰
	fj int32 // 斐济

	// 非洲
	za int32 // 南非
	eg int32 // 埃及
	ng int32 // 尼日利亚
	ke int32 // 肯尼亚
	ma int32 // 摩洛哥
	gh int32 // 加纳
	tz int32 // 坦桑尼亚
	et int32 // 埃塞俄比亚
	tn int32 // 突尼斯

	// 其他
	other int32
	ap    int32 // 亚太地区
}

var counter Counter

// countryMap 存储所有支持的国家信息
var countryMap map[string]*CountryInfo

// 初始化国家信息映射表
func init() {
	countryMap = make(map[string]*CountryInfo)

	// 添加国家信息
	addCountry("hk", "🇭🇰", "香港", `(?i)(\bhk\b|港|hongkong|hong kong)`, &counter.hk)
	addCountry("tw", "🇹🇼", "台湾", `(?i)(\btw\b|台|taiwan|tai wen)`, &counter.tw)
	addCountry("us", "🇺🇸", "美国", `(?i)(\bus\b|美|united states|america)`, &counter.us)
	addCountry("sg", "🇸🇬", "新加坡", `(?i)(\bsg\b|新|singapore|狮城)`, &counter.sg)
	addCountry("jp", "🇯🇵", "日本", `(?i)(\bjp\b|日|japan)`, &counter.jp)
	addCountry("uk", "🇬🇧", "英国", `(?i)(\buk\b|英|united kingdom|britain|\bgb\b)`, &counter.uk)
	addCountry("ca", "🇨🇦", "加拿大", `(?i)(\bca\b|加|canada)`, &counter.ca)
	addCountry("au", "🇦🇺", "澳大利亚", `(?i)(\bau\b|澳|australia)`, &counter.au)
	addCountry("de", "🇩🇪", "德国", `(?i)(\bde\b|德|germany|deutschland)`, &counter.de)
	addCountry("fr", "🇫🇷", "法国", `(?i)(\bfr\b|法|france)`, &counter.fr)
	addCountry("nl", "🇳🇱", "荷兰", `(?i)(\bnl\b|荷|netherlands)`, &counter.nl)
	addCountry("ru", "🇷🇺", "俄罗斯", `(?i)(\bru\b|俄|russia)`, &counter.ru)
	addCountry("hu", "🇭🇺", "匈牙利", `(?i)(\bhu\b|匈|hungary)`, &counter.hu)
	addCountry("ua", "🇺🇦", "乌克兰", `(?i)(\bua\b|乌|ukraine)`, &counter.ua)
	addCountry("pl", "🇵🇱", "波兰", `(?i)(\bpl\b|波|poland)`, &counter.pl)
	addCountry("kr", "🇰🇷", "韩国", `(?i)(\bkr\b|韩|korea)`, &counter.kr)
	addCountry("ap", "🌏", "亚太地区", `(?i)(\bap\b|亚太|asia)`, &counter.ap)
	addCountry("ir", "🇮🇷", "伊朗", `(?i)(\bir\b|伊|iran)`, &counter.ir)
	addCountry("it", "🇮🇹", "意大利", `(?i)(\bit\b|意|italy)`, &counter.it)
	addCountry("fi", "🇫🇮", "芬兰", `(?i)(\bfi\b|芬|finland)`, &counter.fi)
	addCountry("kh", "🇰🇭", "柬埔寨", `(?i)(\bkh\b|柬|cambodia)`, &counter.kh)
	addCountry("br", "🇧🇷", "巴西", `(?i)(\bbr\b|巴|brazil)`, &counter.br)
	addCountry("in", "🇮🇳", "印度", `(?i)(\bin\b|印|india)`, &counter.in)
	addCountry("ae", "🇦🇪", "阿拉伯酋长国", `(?i)(\bae\b|阿|\buae\b|阿拉伯酋长国)`, &counter.ae)
	addCountry("ch", "🇨🇭", "瑞士", `(?i)(\bch\b|瑞士|switzerland)`, &counter.ch)

	// 新增国家
	addCountry("pt", "🇵🇹", "葡萄牙", `(?i)(\bpt\b|葡|portugal)`, &counter.pt)
	addCountry("es", "🇪🇸", "西班牙", `(?i)(\bes\b|西|spain|españa)`, &counter.es)
	addCountry("tr", "🇹🇷", "土耳其", `(?i)(\btr\b|土|turkey|türkiye)`, &counter.tr)
	addCountry("ar", "🇦🇷", "阿根廷", `(?i)(\bar\b|阿根廷|argentina)`, &counter.ar)
	addCountry("mx", "🇲🇽", "墨西哥", `(?i)(\bmx\b|墨西哥|mexico)`, &counter.mx)
	addCountry("za", "🇿🇦", "南非", `(?i)(\bza\b|南非|south africa)`, &counter.za)
	addCountry("gr", "🇬🇷", "希腊", `(?i)(\bgr\b|希腊|greece)`, &counter.gr)
	addCountry("no", "🇳🇴", "挪威", `(?i)(\bno\b|挪威|norway)`, &counter.no)
	addCountry("se", "🇸🇪", "瑞典", `(?i)(\bse\b|瑞典|sweden)`, &counter.se)
	addCountry("dk", "🇩🇰", "丹麦", `(?i)(\bdk\b|丹麦|denmark)`, &counter.dk)
	addCountry("at", "🇦🇹", "奥地利", `(?i)(\bat\b|奥地利|austria)`, &counter.at)
	addCountry("be", "🇧🇪", "比利时", `(?i)(\bbe\b|比利时|belgium)`, &counter.be)
	addCountry("nz", "🇳🇿", "新西兰", `(?i)(\bnz\b|新西兰|new zealand)`, &counter.nz)
	addCountry("ie", "🇮🇪", "爱尔兰", `(?i)(\bie\b|爱尔兰|ireland)`, &counter.ie)
	addCountry("my", "🇲🇾", "马来西亚", `(?i)(\bmy\b|马来西亚|malaysia)`, &counter.my)
	addCountry("th", "🇹🇭", "泰国", `(?i)(\bth\b|泰国|thailand)`, &counter.th)
	addCountry("vn", "🇻🇳", "越南", `(?i)(\bvn\b|越南|vietnam)`, &counter.vn)
	addCountry("ph", "🇵🇭", "菲律宾", `(?i)(\bph\b|菲律宾|philippines)`, &counter.ph)
	addCountry("il", "🇮🇱", "以色列", `(?i)(\bil\b|以色列|israel)`, &counter.il)
	addCountry("cl", "🇨🇱", "智利", `(?i)(\bcl\b|智利|chile)`, &counter.cl)
	addCountry("co", "🇨🇴", "哥伦比亚", `(?i)(\bco\b|哥伦比亚|colombia)`, &counter.co)

	// 新增更多国家
	// 欧洲
	addCountry("ro", "🇷🇴", "罗马尼亚", `(?i)(\bro\b|罗马尼亚|romania)`, &counter.ro)
	addCountry("bg", "🇧🇬", "保加利亚", `(?i)(\bbg\b|保加利亚|bulgaria)`, &counter.bg)
	addCountry("hr", "🇭🇷", "克罗地亚", `(?i)(\bhr\b|克罗地亚|croatia)`, &counter.hr)
	addCountry("rs", "🇷🇸", "塞尔维亚", `(?i)(\brs\b|塞尔维亚|serbia)`, &counter.rs)
	addCountry("sk", "🇸🇰", "斯洛伐克", `(?i)(\bsk\b|斯洛伐克|slovakia)`, &counter.sk)
	addCountry("si", "🇸🇮", "斯洛文尼亚", `(?i)(\bsi\b|斯洛文尼亚|slovenia)`, &counter.si)
	addCountry("ee", "🇪🇪", "爱沙尼亚", `(?i)(\bee\b|爱沙尼亚|estonia)`, &counter.ee)
	addCountry("lv", "🇱🇻", "拉脱维亚", `(?i)(\blv\b|拉脱维亚|latvia)`, &counter.lv)
	addCountry("lt", "🇱🇹", "立陶宛", `(?i)(\blt\b|立陶宛|lithuania)`, &counter.lt)
	addCountry("is", "🇮🇸", "冰岛", `(?i)(\bis\b|冰岛|iceland)`, &counter.is)
	addCountry("lu", "🇱🇺", "卢森堡", `(?i)(\blu\b|卢森堡|luxembourg)`, &counter.lu)
	addCountry("mc", "🇲🇨", "摩纳哥", `(?i)(\bmc\b|摩纳哥|monaco)`, &counter.mc)

	// 非洲
	addCountry("eg", "🇪🇬", "埃及", `(?i)(\beg\b|埃及|egypt)`, &counter.eg)
	addCountry("ma", "🇲🇦", "摩洛哥", `(?i)(\bma\b|摩洛哥|morocco)`, &counter.ma)
	addCountry("tn", "🇹🇳", "突尼斯", `(?i)(\btn\b|突尼斯|tunisia)`, &counter.tn)
	addCountry("ng", "🇳🇬", "尼日利亚", `(?i)(\bng\b|尼日利亚|nigeria)`, &counter.ng)
	addCountry("ke", "🇰🇪", "肯尼亚", `(?i)(\bke\b|肯尼亚|kenya)`, &counter.ke)

	// 亚洲
	addCountry("id", "🇮🇩", "印度尼西亚", `(?i)(\bid\b|印尼|印度尼西亚|indonesia)`, &counter.id)
	addCountry("pk", "🇵🇰", "巴基斯坦", `(?i)(\bpk\b|巴基斯坦|pakistan)`, &counter.pk)
	addCountry("bd", "🇧🇩", "孟加拉国", `(?i)(\bbd\b|孟加拉|bangladesh)`, &counter.bd)
	addCountry("np", "🇳🇵", "尼泊尔", `(?i)(\bnp\b|尼泊尔|nepal)`, &counter.np)
	addCountry("lk", "🇱🇰", "斯里兰卡", `(?i)(\blk\b|斯里兰卡|sri lanka)`, &counter.lk)
	addCountry("kz", "🇰🇿", "哈萨克斯坦", `(?i)(\bkz\b|哈萨克斯坦|kazakhstan)`, &counter.kz)
	addCountry("uz", "🇺🇿", "乌兹别克斯坦", `(?i)(\buz\b|乌兹别克斯坦|uzbekistan)`, &counter.uz)

	// 南美洲
	addCountry("pe", "🇵🇪", "秘鲁", `(?i)(\bpe\b|秘鲁|peru)`, &counter.pe)
	addCountry("ec", "🇪🇨", "厄瓜多尔", `(?i)(\bec\b|厄瓜多尔|ecuador)`, &counter.ec)
	addCountry("ve", "🇻🇪", "委内瑞拉", `(?i)(\bve\b|委内瑞拉|venezuela)`, &counter.ve)

	// 北美/中美/加勒比
	addCountry("cu", "🇨🇺", "古巴", `(?i)(\bcu\b|古巴|cuba)`, &counter.cu)
}

// 添加国家信息到映射表
func addCountry(code, emoji, name, pattern string, counter *int32) {
	countryMap[code] = &CountryInfo{
		Emoji:   emoji,
		Name:    name,
		Pattern: pattern,
		Counter: counter,
	}
}

// ResetRenameCounter 重置所有计数器为0
func ResetRenameCounter() {
	counter = Counter{}
}

// Rename 重命名节点
func Rename(name string) string {
	// 遍历所有国家并尝试匹配
	for _, info := range countryMap {
		if regexp.MustCompile(info.Pattern).MatchString(name) {
			atomic.AddInt32(info.Counter, 1)
			return info.Emoji + info.Name + strconv.Itoa(int(atomic.LoadInt32(info.Counter)))
		}
	}

	// 如果没有匹配到任何国家，归类为"其他"
	atomic.AddInt32(&counter.other, 1)
	return "🌀其他" + strconv.Itoa(int(atomic.LoadInt32(&counter.other))) + "-" + name
}
