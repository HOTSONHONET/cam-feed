export namespace detector {
	
	export class Detection {
	    label: string;
	    score: number;
	    box: number[];
	
	    static createFrom(source: any = {}) {
	        return new Detection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.score = source["score"];
	        this.box = source["box"];
	    }
	}

}

export namespace main {
	
	export class HubConfig {
	    Host: string;
	    UseTLS: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HubConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Host = source["Host"];
	        this.UseTLS = source["UseTLS"];
	    }
	}

}

